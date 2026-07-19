package traefik_plugin_state_geo

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

const (
	defaultDatabaseReloadInterval = time.Minute
	minimumDatabaseReloadInterval = time.Second
)

type geoDatabase interface {
	Lookup(net.IP, any) error
}

type databaseFingerprint struct {
	size    int64
	modTime time.Time
}

type databaseSnapshot struct {
	reader  geoDatabase
	version uint64
}

type databaseManager struct {
	path string

	snapshotValue atomic.Value
	fingerprint   databaseFingerprint
	version       uint64

	reloadMutex       sync.Mutex
	reloadInterval    time.Duration
	nextCheckUnixNano atomic.Int64

	now  func() time.Time
	stat func(string) (os.FileInfo, error)
	open func(string) (geoDatabase, error)
}

type databaseManagerRegistry struct {
	mutex    sync.Mutex
	managers map[string]*databaseManager
}

var sharedDatabaseManagers = databaseManagerRegistry{
	managers: make(map[string]*databaseManager),
}

func parseDatabaseReloadInterval(rawInterval string) (time.Duration, error) {
	if rawInterval == "" {
		return defaultDatabaseReloadInterval, nil
	}

	interval, err := time.ParseDuration(rawInterval)
	if err != nil {
		return 0, fmt.Errorf("invalid databaseReloadInterval %q: %w", rawInterval, err)
	}
	if interval < minimumDatabaseReloadInterval {
		return 0, fmt.Errorf(
			"invalid databaseReloadInterval %q: minimum is %s",
			rawInterval,
			minimumDatabaseReloadInterval,
		)
	}

	return interval, nil
}

func getSharedDatabaseManager(path string, reloadInterval time.Duration) (*databaseManager, error) {
	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("resolve geoip database path %q: %w", path, err)
	}

	sharedDatabaseManagers.mutex.Lock()
	defer sharedDatabaseManagers.mutex.Unlock()

	if manager, exists := sharedDatabaseManagers.managers[absolutePath]; exists {
		manager.useReloadInterval(reloadInterval)
		return manager, nil
	}

	manager := newDatabaseManager(absolutePath, reloadInterval)
	sharedDatabaseManagers.managers[absolutePath] = manager
	return manager, nil
}

func newDatabaseManager(path string, reloadInterval time.Duration) *databaseManager {
	return &databaseManager{
		path:           path,
		reloadInterval: reloadInterval,
		now:            time.Now,
		stat:           os.Stat,
		open: func(path string) (geoDatabase, error) {
			return maxminddb.Open(path)
		},
	}
}

func (m *databaseManager) ensureLoaded() error {
	if m.hasReader() {
		return nil
	}

	m.reloadMutex.Lock()
	defer m.reloadMutex.Unlock()

	if m.hasReader() {
		return nil
	}

	return m.reloadLocked(m.now())
}

func (m *databaseManager) snapshot() (databaseSnapshot, error) {
	reloadErr := m.reloadIfNeeded()
	return m.currentSnapshot(), reloadErr
}

func (m *databaseManager) reloadIfNeeded() error {
	now := m.now()
	if now.UnixNano() < m.nextCheckUnixNano.Load() {
		return nil
	}

	// A reload may read and parse the complete MMDB. Requests that lose this
	// non-blocking election keep using the last known-good immutable reader.
	if !m.reloadMutex.TryLock() {
		return nil
	}
	defer m.reloadMutex.Unlock()

	// Another request may have completed the check between the atomic fast path
	// and this request winning the election.
	now = m.now()
	if now.UnixNano() < m.nextCheckUnixNano.Load() {
		return nil
	}

	return m.reloadLocked(now)
}

func (m *databaseManager) reloadLocked(now time.Time) error {
	m.nextCheckUnixNano.Store(now.Add(m.reloadInterval).UnixNano())

	fileInfo, err := m.stat(m.path)
	if err != nil {
		return fmt.Errorf("stat geoip database: %w", err)
	}
	fingerprint := databaseFingerprint{
		size:    fileInfo.Size(),
		modTime: fileInfo.ModTime(),
	}

	current := m.currentSnapshot()
	hasCurrentReader := current.reader != nil
	isCurrent := hasCurrentReader && m.fingerprint.equal(fingerprint)
	if isCurrent {
		return nil
	}

	reader, err := m.open(m.path)
	if err != nil {
		return fmt.Errorf("open updated geoip database: %w", err)
	}
	if reader == nil {
		return fmt.Errorf("open updated geoip database: reader is nil")
	}

	// Readers are immutable after construction. Publishing one complete snapshot
	// atomically lets in-flight requests retain the old reader without a read lock.
	m.fingerprint = fingerprint
	m.version++
	m.snapshotValue.Store(databaseSnapshot{reader: reader, version: m.version})

	return nil
}

func (m *databaseManager) hasReader() bool {
	return m.currentSnapshot().reader != nil
}

func (m *databaseManager) currentSnapshot() databaseSnapshot {
	value := m.snapshotValue.Load()
	if value == nil {
		return databaseSnapshot{}
	}

	return value.(databaseSnapshot)
}

func (m *databaseManager) useReloadInterval(interval time.Duration) {
	m.reloadMutex.Lock()
	defer m.reloadMutex.Unlock()

	if interval >= m.reloadInterval {
		return
	}

	m.reloadInterval = interval
	m.nextCheckUnixNano.Store(0)
}

func (f databaseFingerprint) equal(other databaseFingerprint) bool {
	return f.size == other.size && f.modTime.Equal(other.modTime)
}

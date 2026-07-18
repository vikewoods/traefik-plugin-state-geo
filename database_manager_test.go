package traefik_plugin_state_geo

import (
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

type fakeDatabaseFileInfo struct {
	size    int64
	modTime time.Time
}

func (f fakeDatabaseFileInfo) Name() string       { return "GeoLite2-City.mmdb" }
func (f fakeDatabaseFileInfo) Size() int64        { return f.size }
func (f fakeDatabaseFileInfo) Mode() os.FileMode  { return 0o444 }
func (f fakeDatabaseFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeDatabaseFileInfo) IsDir() bool        { return false }
func (f fakeDatabaseFileInfo) Sys() any           { return nil }

type fakeGeoDatabase struct {
	generation int
}

func (f *fakeGeoDatabase) Lookup(_ net.IP, _ any) error {
	return nil
}

func TestDatabaseManagerReloadsWhenFileFingerprintChanges(t *testing.T) {
	now := time.Date(2026, time.July, 18, 20, 0, 0, 0, time.UTC)
	fileInfo := fakeDatabaseFileInfo{size: 100, modTime: now}
	openCount := 0

	manager := newDatabaseManager("/data/GeoLite2-City.mmdb", time.Minute)
	manager.now = func() time.Time { return now }
	manager.stat = func(_ string) (os.FileInfo, error) { return fileInfo, nil }
	manager.open = func(_ string) (geoDatabase, error) {
		openCount++
		return &fakeGeoDatabase{generation: openCount}, nil
	}

	if err := manager.ensureLoaded(); err != nil {
		t.Fatalf("ensureLoaded() error = %v", err)
	}
	initial, err := manager.snapshot()
	if err != nil {
		t.Fatalf("snapshot() error = %v", err)
	}
	if initial.version != 1 || openCount != 1 {
		t.Fatalf("initial version/open count = %d/%d, want 1/1", initial.version, openCount)
	}

	now = now.Add(time.Minute)
	unchanged, err := manager.snapshot()
	if err != nil {
		t.Fatalf("unchanged snapshot() error = %v", err)
	}
	if unchanged.version != 1 || openCount != 1 {
		t.Fatalf("unchanged version/open count = %d/%d, want 1/1", unchanged.version, openCount)
	}

	fileInfo = fakeDatabaseFileInfo{size: 101, modTime: now.Add(time.Second)}
	now = now.Add(time.Minute)
	reloaded, err := manager.snapshot()
	if err != nil {
		t.Fatalf("reloaded snapshot() error = %v", err)
	}
	if reloaded.version != 2 || openCount != 2 {
		t.Fatalf("reloaded version/open count = %d/%d, want 2/2", reloaded.version, openCount)
	}

	reader, ok := reloaded.reader.(*fakeGeoDatabase)
	if !ok {
		t.Fatalf("reloaded reader type = %T, want *fakeGeoDatabase", reloaded.reader)
	}
	if reader.generation != 2 {
		t.Fatalf("reloaded reader generation = %d, want 2", reader.generation)
	}
}

func TestDatabaseManagerKeepsLastGoodReaderWhenReloadFails(t *testing.T) {
	now := time.Date(2026, time.July, 18, 20, 0, 0, 0, time.UTC)
	fileInfo := fakeDatabaseFileInfo{size: 100, modTime: now}
	openCount := 0

	manager := newDatabaseManager("/data/GeoLite2-City.mmdb", time.Minute)
	manager.now = func() time.Time { return now }
	manager.stat = func(_ string) (os.FileInfo, error) { return fileInfo, nil }
	manager.open = func(_ string) (geoDatabase, error) {
		openCount++
		if openCount > 1 {
			return nil, errors.New("replacement is incomplete")
		}
		return &fakeGeoDatabase{generation: openCount}, nil
	}

	if err := manager.ensureLoaded(); err != nil {
		t.Fatalf("ensureLoaded() error = %v", err)
	}
	initial, _ := manager.snapshot()

	fileInfo = fakeDatabaseFileInfo{size: 50, modTime: now.Add(time.Second)}
	now = now.Add(time.Minute)
	afterFailure, reloadErr := manager.snapshot()
	if reloadErr == nil {
		t.Fatal("snapshot() reload error = nil, want replacement error")
	}
	if afterFailure.reader != initial.reader {
		t.Fatal("failed reload replaced the last known-good reader")
	}
	if afterFailure.version != initial.version {
		t.Fatalf("version after failed reload = %d, want %d", afterFailure.version, initial.version)
	}

	_, immediateErr := manager.snapshot()
	if immediateErr != nil {
		t.Fatalf("immediate snapshot repeated bounded reload error: %v", immediateErr)
	}
	if openCount != 2 {
		t.Fatalf("open count = %d, want 2", openCount)
	}
}

func TestDatabaseManagerRecoversAfterInitialLoadFailure(t *testing.T) {
	now := time.Date(2026, time.July, 18, 20, 0, 0, 0, time.UTC)
	fileInfo := fakeDatabaseFileInfo{size: 100, modTime: now}
	openCount := 0

	manager := newDatabaseManager("/data/GeoLite2-City.mmdb", time.Minute)
	manager.now = func() time.Time { return now }
	manager.stat = func(_ string) (os.FileInfo, error) { return fileInfo, nil }
	manager.open = func(_ string) (geoDatabase, error) {
		openCount++
		if openCount == 1 {
			return nil, errors.New("database is not ready")
		}
		return &fakeGeoDatabase{generation: openCount}, nil
	}

	if err := manager.ensureLoaded(); err == nil {
		t.Fatal("ensureLoaded() error = nil, want initial load error")
	}

	now = now.Add(time.Minute)
	recovered, err := manager.snapshot()
	if err != nil {
		t.Fatalf("recovery snapshot() error = %v", err)
	}
	if recovered.reader == nil || recovered.version != 1 {
		t.Fatalf("recovered reader/version = %v/%d, want non-nil/1", recovered.reader, recovered.version)
	}
}

func TestSharedDatabaseManagerUsesOneInstancePerPath(t *testing.T) {
	path := t.TempDir() + "/GeoLite2-City.mmdb"
	first, err := getSharedDatabaseManager(path, time.Minute)
	if err != nil {
		t.Fatalf("getSharedDatabaseManager() first error = %v", err)
	}
	second, err := getSharedDatabaseManager(path, 30*time.Second)
	if err != nil {
		t.Fatalf("getSharedDatabaseManager() second error = %v", err)
	}

	if first != second {
		t.Fatal("same database path returned different shared managers")
	}
	if first.reloadInterval != 30*time.Second {
		t.Fatalf("shared reload interval = %s, want %s", first.reloadInterval, 30*time.Second)
	}
}

func TestDatabaseManagerConcurrentSnapshotsAndReloads(t *testing.T) {
	baseTime := time.Date(2026, time.July, 18, 20, 0, 0, 0, time.UTC)
	var fixtureMutex sync.Mutex
	now := baseTime
	fileInfo := fakeDatabaseFileInfo{size: 100, modTime: baseTime}
	openCount := 0

	manager := newDatabaseManager("/data/GeoLite2-City.mmdb", time.Nanosecond)
	manager.now = func() time.Time {
		fixtureMutex.Lock()
		defer fixtureMutex.Unlock()
		return now
	}
	manager.stat = func(_ string) (os.FileInfo, error) {
		fixtureMutex.Lock()
		defer fixtureMutex.Unlock()
		return fileInfo, nil
	}
	manager.open = func(_ string) (geoDatabase, error) {
		fixtureMutex.Lock()
		defer fixtureMutex.Unlock()
		openCount++
		return &fakeGeoDatabase{generation: openCount}, nil
	}
	if err := manager.ensureLoaded(); err != nil {
		t.Fatalf("ensureLoaded() error = %v", err)
	}

	var waitGroup sync.WaitGroup
	for worker := 0; worker < 12; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for iteration := 0; iteration < 100; iteration++ {
				snapshot, _ := manager.snapshot()
				if snapshot.reader == nil {
					t.Error("concurrent snapshot returned nil reader")
					return
				}
			}
		}()
	}

	for iteration := 1; iteration <= 20; iteration++ {
		fixtureMutex.Lock()
		now = now.Add(time.Second)
		fileInfo = fakeDatabaseFileInfo{
			size:    int64(100 + iteration),
			modTime: now,
		}
		fixtureMutex.Unlock()
		_, _ = manager.snapshot()
	}
	waitGroup.Wait()
}

func TestDatabaseReloadIntervalValidation(t *testing.T) {
	tests := []struct {
		name     string
		interval string
	}{
		{name: "invalid duration", interval: "soon"},
		{name: "zero duration", interval: "0s"},
		{name: "below minimum", interval: "999ms"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseDatabaseReloadInterval(test.interval); err == nil {
				t.Fatal("parseDatabaseReloadInterval() error = nil, want configuration error")
			}
		})
	}
}

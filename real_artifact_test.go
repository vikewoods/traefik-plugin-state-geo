package traefik_plugin_state_geo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

const (
	realArtifactEnvironment = "STATE_GEO_REAL_MMDB"
	realSmokeCasesFile      = "STATE_GEO_REAL_SMOKE_CASES_FILE"
)

type realArtifactEvidence struct {
	fileInfo os.FileInfo
	size     int64
	mode     os.FileMode
	modTime  time.Time
	checksum [sha256.Size]byte
}

type realArtifactRepresentative struct {
	address     netip.Addr
	prefix      netip.Prefix
	country     string
	subdivision string
}

type realArtifactCases struct {
	usIPv4          realArtifactRepresentative
	usIPv6          realArtifactRepresentative
	nonUSIPv4       realArtifactRepresentative
	nonUSIPv6       realArtifactRepresentative
	usWithoutState  realArtifactRepresentative
	unknownLocation realArtifactRepresentative
	allowedUSState  realArtifactRepresentative
	blockedUSState  realArtifactRepresentative
}

func TestRealComplianceArtifactEnvironment(t *testing.T) {
	tests := []struct {
		name           string
		value          string
		present        bool
		expectedPath   string
		expectedEnable bool
		expectedError  string
	}{
		{name: "absent"},
		{
			name:           "empty",
			present:        true,
			expectedEnable: true,
			expectedError:  "STATE_GEO_REAL_MMDB is set but empty",
		},
		{
			name:           "configured",
			value:          "/private/example/stategeodb.mmdb",
			present:        true,
			expectedPath:   "/private/example/stategeodb.mmdb",
			expectedEnable: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path, enabled, err := parseRealArtifactEnvironment(test.value, test.present)
			if path != test.expectedPath || enabled != test.expectedEnable {
				t.Errorf("parseRealArtifactEnvironment() = %q, %t; want %q, %t", path, enabled, test.expectedPath, test.expectedEnable)
			}
			actualError := ""
			if err != nil {
				actualError = err.Error()
			}
			if actualError != test.expectedError {
				t.Errorf("parseRealArtifactEnvironment() error = %q, want %q", actualError, test.expectedError)
			}
		})
	}
}

func TestRealComplianceArtifact(t *testing.T) {
	path := requireRealArtifactPath(t)
	reader, evidence := openRealArtifact(t, path)
	defer func() {
		if !realArtifactUnchanged(path, evidence) {
			t.Error("external compliance artifact changed during testing")
		}
	}()

	cases := discoverRealArtifactCases(t, reader)
	assertRealArtifactRecords(t, reader, cases)
	assertRealArtifactDecisionMatrix(t, path, cases)
	assertRealArtifactReloadLifecycle(t, path, cases.usIPv4)
}

func TestRealComplianceArtifactSmokeCases(t *testing.T) {
	outputPath, configured := os.LookupEnv(realSmokeCasesFile)
	if !configured {
		t.Skip(realSmokeCasesFile + " is not set")
	}
	if outputPath == "" {
		t.Fatal("STATE_GEO_REAL_SMOKE_CASES_FILE is set but empty")
	}

	path := requireRealArtifactPath(t)
	reader, evidence := openRealArtifact(t, path)
	defer func() {
		if !realArtifactUnchanged(path, evidence) {
			t.Error("external compliance artifact changed during smoke-case discovery")
		}
	}()

	cases := discoverRealArtifactCases(t, reader)
	contents := fmt.Sprintf(
		"allowed_us=%s\nblocked_us=%s\nblocked_state=%s\nus_ipv6=%s\nus_ipv6_state=%s\n"+
			"non_us_ipv4=%s\nnon_us_ipv6=%s\nartifact_size=%d\nartifact_mode=%o\n"+
			"artifact_mtime=%d\nartifact_sha256=%x\n",
		cases.allowedUSState.address,
		cases.blockedUSState.address,
		cases.blockedUSState.subdivision,
		cases.usIPv6.address,
		cases.usIPv6.subdivision,
		cases.nonUSIPv4.address,
		cases.nonUSIPv6.address,
		evidence.size,
		evidence.mode.Perm(),
		evidence.modTime.UnixNano(),
		evidence.checksum,
	)
	// #nosec G304 -- the opt-in smoke harness supplies an exclusive test-output path.
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("real-artifact smoke case file could not be created")
	}
	if _, err := io.WriteString(file, contents); err != nil {
		_ = file.Close()
		t.Fatal("real-artifact smoke cases could not be written")
	}
	if err := file.Close(); err != nil {
		t.Fatal("real-artifact smoke case file could not be closed")
	}
}

func parseRealArtifactEnvironment(value string, present bool) (string, bool, error) {
	if !present {
		return "", false, nil
	}
	if value == "" {
		return "", true, errors.New("STATE_GEO_REAL_MMDB is set but empty")
	}
	return value, true, nil
}

func requireRealArtifactPath(t *testing.T) string {
	t.Helper()
	value, present := os.LookupEnv(realArtifactEnvironment)
	path, enabled, err := parseRealArtifactEnvironment(value, present)
	if !enabled {
		t.Skip(realArtifactEnvironment + " is not set")
	}
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func openRealArtifact(t *testing.T, path string) (*maxminddb.Reader, realArtifactEvidence) {
	t.Helper()
	fileInfo, err := os.Lstat(path)
	if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() <= 0 {
		t.Fatal("external compliance artifact is not a readable regular file")
	}

	checksum, ok := hashRealArtifact(path, fileInfo)
	if !ok {
		t.Fatal("external compliance artifact could not be hashed safely")
	}
	reader, err := maxminddb.Open(path)
	if err != nil {
		t.Fatal("external compliance artifact could not be opened")
	}
	if err := reader.Verify(); err != nil {
		t.Fatal("external compliance artifact failed structural verification")
	}
	metadata := reader.Metadata
	if metadata.DatabaseType != "StateGeo-Country-USSubdivision" ||
		metadata.RecordSize != 24 ||
		metadata.IPVersion != 6 ||
		metadata.BuildEpoch == 0 ||
		metadata.BinaryFormatMajorVersion != 2 ||
		metadata.BinaryFormatMinorVersion != 0 {
		t.Fatal("external compliance artifact metadata is incompatible")
	}

	return reader, realArtifactEvidence{
		fileInfo: fileInfo,
		size:     fileInfo.Size(),
		mode:     fileInfo.Mode(),
		modTime:  fileInfo.ModTime(),
		checksum: checksum,
	}
}

func hashRealArtifact(path string, expected os.FileInfo) ([sha256.Size]byte, bool) {
	// #nosec G304 -- the opt-in test deliberately reads its operator-supplied artifact path.
	file, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, false
	}
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(expected, fileInfo) || fileInfo.Size() != expected.Size() {
		_ = file.Close()
		return [sha256.Size]byte{}, false
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		_ = file.Close()
		return [sha256.Size]byte{}, false
	}
	if err := file.Close(); err != nil {
		return [sha256.Size]byte{}, false
	}
	var checksum [sha256.Size]byte
	copy(checksum[:], hash.Sum(nil))
	return checksum, true
}

func realArtifactUnchanged(path string, expected realArtifactEvidence) bool {
	fileInfo, err := os.Lstat(path)
	if err != nil || !os.SameFile(expected.fileInfo, fileInfo) ||
		fileInfo.Size() != expected.size || fileInfo.Mode() != expected.mode ||
		!fileInfo.ModTime().Equal(expected.modTime) {
		return false
	}
	checksum, ok := hashRealArtifact(path, expected.fileInfo)
	return ok && checksum == expected.checksum
}

func discoverRealArtifactCases(t *testing.T, reader *maxminddb.Reader) realArtifactCases {
	t.Helper()
	var cases realArtifactCases
	usStateCandidates := make([]realArtifactRepresentative, 0, 8)
	seenUSStates := make(map[string]struct{}, 8)
	networks := reader.Networks(maxminddb.SkipAliasedNetworks)
	for networks.Next() {
		var record geoRecord
		network, err := networks.Network(&record)
		if err != nil {
			t.Fatal("external compliance artifact traversal failed")
		}
		representative, ok := representativeForNetwork(network, record)
		if !ok {
			continue
		}
		if representative.country == "US" && representative.subdivision != "" {
			if _, seen := seenUSStates[representative.subdivision]; !seen {
				seenUSStates[representative.subdivision] = struct{}{}
				usStateCandidates = append(usStateCandidates, representative)
			}
		}

		switch {
		case representative.country == "US" && representative.subdivision != "" && representative.address.Is4():
			setRepresentative(&cases.usIPv4, representative)
		case representative.country == "US" && representative.subdivision != "" && representative.address.Is6():
			setRepresentative(&cases.usIPv6, representative)
		case representative.country != "" && representative.country != "US" && representative.address.Is4():
			setRepresentative(&cases.nonUSIPv4, representative)
		case representative.country != "" && representative.country != "US" && representative.address.Is6():
			setRepresentative(&cases.nonUSIPv6, representative)
		case representative.country == "US" && representative.subdivision == "":
			setRepresentative(&cases.usWithoutState, representative)
		case representative.country == "" && representative.subdivision == "":
			setRepresentative(&cases.unknownLocation, representative)
		}

		if !cases.blockedUSState.address.IsValid() && cases.usIPv4.address.IsValid() {
			cases.blockedUSState = cases.usIPv4
		}
		if !cases.allowedUSState.address.IsValid() && cases.usIPv4.address.IsValid() && cases.usIPv6.address.IsValid() {
			for _, candidate := range usStateCandidates {
				if candidate.subdivision != cases.usIPv4.subdivision &&
					candidate.subdivision != cases.usIPv6.subdivision {
					cases.allowedUSState = candidate
					break
				}
			}
		}

		if cases.complete() {
			break
		}
	}
	if err := networks.Err(); err != nil {
		t.Fatal("external compliance artifact traversal failed")
	}
	if !cases.complete() {
		t.Fatal("external compliance artifact lacks the required representative matrix")
	}
	return cases
}

func representativeForNetwork(network *net.IPNet, record geoRecord) (realArtifactRepresentative, bool) {
	ones, bits := network.Mask.Size()
	address, ok := netip.AddrFromSlice(network.IP)
	if !ok || ones < 0 {
		return realArtifactRepresentative{}, false
	}
	if bits == 32 {
		address = address.Unmap()
	}
	prefix := netip.PrefixFrom(address, ones).Masked()
	if prefix.Addr() != address.Unmap() {
		return realArtifactRepresentative{}, false
	}

	address, ok = firstPublicAddress(prefix)
	if !ok {
		return realArtifactRepresentative{}, false
	}
	subdivision := ""
	if len(record.Subdivisions) > 0 {
		subdivision = strings.ToUpper(strings.TrimSpace(record.Subdivisions[0].IsoCode))
	}
	return realArtifactRepresentative{
		address:     address,
		prefix:      prefix,
		country:     strings.ToUpper(strings.TrimSpace(record.Country.IsoCode)),
		subdivision: subdivision,
	}, true
}

func firstPublicAddress(prefix netip.Prefix) (netip.Addr, bool) {
	address := prefix.Addr()
	for attempts := 0; attempts < 4096 && address.IsValid() && prefix.Contains(address); attempts++ {
		if address.IsGlobalUnicast() && !isPolicyControlledNonPublicAddress(address) {
			return address, true
		}
		address = address.Next()
	}
	return netip.Addr{}, false
}

func setRepresentative(destination *realArtifactRepresentative, candidate realArtifactRepresentative) {
	if !destination.address.IsValid() {
		*destination = candidate
	}
}

func (cases realArtifactCases) complete() bool {
	return cases.usIPv4.address.IsValid() && cases.usIPv6.address.IsValid() &&
		cases.nonUSIPv4.address.IsValid() && cases.nonUSIPv6.address.IsValid() &&
		cases.usWithoutState.address.IsValid() && cases.unknownLocation.address.IsValid() &&
		cases.allowedUSState.address.IsValid() && cases.blockedUSState.address.IsValid()
}

func assertRealArtifactRecords(t *testing.T, reader *maxminddb.Reader, cases realArtifactCases) {
	t.Helper()
	tests := []struct {
		name           string
		representative realArtifactRepresentative
	}{
		{name: "US IPv4 with subdivision", representative: cases.usIPv4},
		{name: "US IPv6 with subdivision", representative: cases.usIPv6},
		{name: "non-US IPv4 country only", representative: cases.nonUSIPv4},
		{name: "non-US IPv6 country only", representative: cases.nonUSIPv6},
		{name: "US without subdivision", representative: cases.usWithoutState},
		{name: "present unknown location", representative: cases.unknownLocation},
	}
	for _, test := range tests {
		t.Run("record "+test.name, func(t *testing.T) {
			var record geoRecord
			network, found, err := reader.LookupNetwork(net.IP(test.representative.address.AsSlice()), &record)
			if err != nil || !found || network == nil {
				t.Fatal("direct representative lookup failed")
			}
			country := strings.ToUpper(strings.TrimSpace(record.Country.IsoCode))
			subdivision := ""
			if len(record.Subdivisions) > 0 {
				subdivision = strings.ToUpper(strings.TrimSpace(record.Subdivisions[0].IsoCode))
			}
			if country != test.representative.country || subdivision != test.representative.subdivision {
				t.Fatal("direct representative lookup changed record semantics")
			}
			if country != "US" && subdivision != "" {
				t.Fatal("non-US representative unexpectedly requires a subdivision")
			}
		})
	}
}

func assertRealArtifactDecisionMatrix(t *testing.T, path string, cases realArtifactCases) {
	t.Helper()
	tests := []struct {
		name           string
		representative realArtifactRepresentative
		blockedStates  []string
		whitelistedIPs []string
		expectedStatus int
	}{
		{name: "known non-US IPv4 denied", representative: cases.nonUSIPv4, expectedStatus: http.StatusForbidden},
		{name: "known non-US IPv6 denied", representative: cases.nonUSIPv6, expectedStatus: http.StatusForbidden},
		{name: "US blocked subdivision denied", representative: cases.blockedUSState, blockedStates: []string{cases.blockedUSState.subdivision}, expectedStatus: http.StatusForbidden},
		{name: "US unblocked subdivision reaches next", representative: cases.allowedUSState, blockedStates: []string{cases.blockedUSState.subdivision}, expectedStatus: http.StatusNoContent},
		{name: "US IPv6 blocked subdivision denied", representative: cases.usIPv6, blockedStates: []string{cases.usIPv6.subdivision}, expectedStatus: http.StatusForbidden},
		{name: "US IPv6 unblocked subdivision reaches next", representative: cases.usIPv6, blockedStates: []string{"ZZ"}, expectedStatus: http.StatusNoContent},
		{name: "US unknown subdivision denied", representative: cases.usWithoutState, expectedStatus: http.StatusForbidden},
		{name: "present unknown country denied", representative: cases.unknownLocation, expectedStatus: http.StatusForbidden},
		{name: "exact IP whitelist bypasses geography", representative: cases.nonUSIPv4, whitelistedIPs: []string{cases.nonUSIPv4.address.String()}, expectedStatus: http.StatusNoContent},
		{name: "CIDR whitelist bypasses geography", representative: cases.nonUSIPv6, whitelistedIPs: []string{cases.nonUSIPv6.prefix.String()}, expectedStatus: http.StatusNoContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := strictRealArtifactConfig(path)
			config.BlockedStates = test.blockedStates
			config.WhitelistedIPs = test.whitelistedIPs
			handler := newRealArtifactMiddleware(t, config)
			request := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			request.RemoteAddr = net.JoinHostPort(test.representative.address.String(), "443")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.expectedStatus {
				t.Errorf("status = %d, want %d", response.Code, test.expectedStatus)
			}
		})
	}
}

func strictRealArtifactConfig(path string) *Config {
	config := CreateConfig()
	config.DBPath = path
	config.DatabaseReloadInterval = "1h"
	config.CacheSize = 0
	config.CacheTTL = "1m"
	config.ClientIPHeaders = []string{}
	config.TrustedProxyCIDRs = []string{}
	config.RejectInvalidClientIPHeaders = true
	config.BlockNonUS = true
	config.BlockUSStates = true
	config.DatabaseFailurePolicy = "deny"
	config.LookupFailurePolicy = "deny"
	config.InvalidClientIPPolicy = "deny"
	config.UnknownCountryPolicy = "deny"
	config.UnknownSubdivisionPolicy = "deny"
	config.PrivateIPPolicy = "deny"
	config.LogLevel = "off"
	return config
}

func newRealArtifactMiddleware(t *testing.T, config *Config) *stateBlock {
	t.Helper()
	next := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})
	handler, err := New(context.Background(), next, config, "real-compliance-artifact")
	if err != nil {
		t.Fatal("real compliance middleware construction failed")
	}
	middleware, ok := handler.(*stateBlock)
	if !ok {
		t.Fatal("real compliance middleware has unexpected type")
	}
	return middleware
}

func assertRealArtifactReloadLifecycle(t *testing.T, sourcePath string, representative realArtifactRepresentative) {
	t.Helper()
	directory := t.TempDir()
	livePath := filepath.Join(directory, "stategeodb.mmdb")
	baseTime := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	copyRealArtifact(t, sourcePath, livePath, baseTime)

	now := baseTime
	manager := newDatabaseManager(livePath, time.Second)
	manager.now = func() time.Time { return now }

	if err := manager.ensureLoaded(); err != nil {
		t.Fatal("test-owned compliance artifact initial load failed")
	}
	initial, err := manager.snapshot()
	if err != nil || initial.reader == nil || initial.version != 1 {
		t.Fatal("test-owned compliance artifact initial snapshot is invalid")
	}
	assertGeoDatabaseLookup(t, initial.reader, representative)

	config := strictRealArtifactConfig("")
	config.CacheSize = 2
	handler := newRealArtifactMiddleware(t, config)
	handler.dbManager = manager
	handler.cache.now = func() time.Time { return now }
	cacheKey := representative.address.String()
	expectedDecision := cacheEntry{allowed: true, stateCode: representative.subdivision}
	if status := serveRealArtifactRequest(handler, representative); status != http.StatusNoContent {
		t.Fatalf("initial real-artifact request status = %d, want %d", status, http.StatusNoContent)
	}
	assertRealArtifactCache(t, handler.cache, cacheKey, initial.version, expectedDecision)
	handler.cache.set(cacheKey, initial.version, cacheEntry{allowed: false, stateCode: "ZZ"})

	atomicReplaceRealArtifact(t, sourcePath, livePath, baseTime.Add(2*time.Second), nil)
	now = baseTime.Add(3 * time.Second)
	if status := serveRealArtifactRequest(handler, representative); status != http.StatusNoContent {
		t.Fatalf("reloaded real-artifact request status = %d, want %d", status, http.StatusNoContent)
	}
	reloaded := manager.currentSnapshot()
	if reloaded.reader == nil || reloaded.reader == initial.reader || reloaded.version != 2 {
		t.Fatal("byte-identical atomic replacement did not advance the database generation")
	}
	assertGeoDatabaseLookup(t, reloaded.reader, representative)
	assertRealArtifactCache(t, handler.cache, cacheKey, reloaded.version, expectedDecision)
	sentinelDecision := cacheEntry{allowed: true, stateCode: "sentinel"}
	handler.cache.set(cacheKey, reloaded.version, sentinelDecision)
	assertRealArtifactCache(t, handler.cache, cacheKey, reloaded.version, sentinelDecision)

	atomicReplaceRealArtifact(t, "", livePath, baseTime.Add(4*time.Second), []byte("malformed mmdb replacement"))
	now = baseTime.Add(5 * time.Second)
	if status := serveRealArtifactRequest(handler, representative); status != http.StatusNoContent {
		t.Fatalf("last-known-good request status = %d, want %d", status, http.StatusNoContent)
	}
	afterFailure := manager.currentSnapshot()
	if afterFailure.reader != reloaded.reader || afterFailure.version != reloaded.version {
		t.Fatal("malformed replacement displaced the last known-good reader")
	}
	assertGeoDatabaseLookup(t, afterFailure.reader, representative)
	assertRealArtifactCache(t, handler.cache, cacheKey, afterFailure.version, sentinelDecision)
	handler.cache.set(cacheKey, afterFailure.version, cacheEntry{allowed: false, stateCode: "ZZ"})

	atomicReplaceRealArtifact(t, sourcePath, livePath, baseTime.Add(6*time.Second), nil)
	now = baseTime.Add(7 * time.Second)
	if status := serveRealArtifactRequest(handler, representative); status != http.StatusNoContent {
		t.Fatalf("recovered real-artifact request status = %d, want %d", status, http.StatusNoContent)
	}
	recovered := manager.currentSnapshot()
	if recovered.reader == nil || recovered.reader == reloaded.reader || recovered.version != 3 {
		t.Fatal("later valid atomic replacement did not recover")
	}
	assertGeoDatabaseLookup(t, recovered.reader, representative)
	assertRealArtifactCache(t, handler.cache, cacheKey, recovered.version, expectedDecision)
}

func serveRealArtifactRequest(handler http.Handler, representative realArtifactRepresentative) int {
	request := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	request.RemoteAddr = net.JoinHostPort(representative.address.String(), "443")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code
}

func assertRealArtifactCache(
	t *testing.T,
	cache *decisionCache,
	key string,
	version uint64,
	expected cacheEntry,
) {
	t.Helper()
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	element, found := cache.entries[key]
	if cache.databaseVersion != version || len(cache.entries) != 1 || !found {
		t.Fatalf(
			"real-artifact cache version/entries/key = %d/%d/%t, want %d/1/true",
			cache.databaseVersion,
			len(cache.entries),
			found,
			version,
		)
	}
	item, ok := element.Value.(decisionCacheItem)
	if !ok || item.decision != expected {
		t.Fatalf("real-artifact cached decision = %+v/%t, want %+v/true", item.decision, ok, expected)
	}
}

func copyRealArtifact(t *testing.T, sourcePath, destinationPath string, modTime time.Time) {
	t.Helper()
	// #nosec G304 -- sourcePath is the already-validated opt-in artifact path.
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal("external compliance artifact could not be opened for test-owned copy")
	}
	// #nosec G304 -- destinationPath is constructed beneath this test's t.TempDir.
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = source.Close()
		t.Fatal("test-owned compliance artifact could not be created")
	}
	_, copyErr := io.Copy(destination, source)
	closeDestinationErr := destination.Close()
	closeSourceErr := source.Close()
	if copyErr != nil || closeDestinationErr != nil || closeSourceErr != nil {
		t.Fatal("test-owned compliance artifact copy failed")
	}
	if err := os.Chtimes(destinationPath, modTime, modTime); err != nil {
		t.Fatal("test-owned compliance artifact timestamp setup failed")
	}
}

func atomicReplaceRealArtifact(t *testing.T, sourcePath, livePath string, modTime time.Time, contents []byte) {
	t.Helper()
	sibling, err := os.CreateTemp(filepath.Dir(livePath), ".stategeodb-replacement-")
	if err != nil {
		t.Fatal("atomic replacement sibling could not be created")
	}
	siblingPath := sibling.Name()
	defer func() { _ = os.Remove(siblingPath) }()

	if contents != nil {
		_, err = sibling.Write(contents)
	} else {
		// #nosec G304 -- sourcePath is the already-validated opt-in artifact path.
		source, openErr := os.Open(sourcePath)
		if openErr != nil {
			_ = sibling.Close()
			t.Fatal("external compliance artifact could not be reopened for replacement")
		}
		_, err = io.Copy(sibling, source)
		closeSourceErr := source.Close()
		if err == nil {
			err = closeSourceErr
		}
	}
	if closeErr := sibling.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal("atomic replacement sibling could not be written")
	}
	if err := os.Chmod(siblingPath, 0o600); err != nil {
		t.Fatal("atomic replacement sibling mode setup failed")
	}
	if err := os.Chtimes(siblingPath, modTime, modTime); err != nil {
		t.Fatal("atomic replacement sibling timestamp setup failed")
	}
	if err := os.Rename(siblingPath, livePath); err != nil {
		t.Fatal("atomic replacement rename failed")
	}
}

func assertGeoDatabaseLookup(t *testing.T, database geoDatabase, representative realArtifactRepresentative) {
	t.Helper()
	var record geoRecord
	if err := database.Lookup(net.IP(representative.address.AsSlice()), &record); err != nil {
		t.Fatal("last known-good real-artifact reader lookup failed")
	}
	country := strings.ToUpper(strings.TrimSpace(record.Country.IsoCode))
	if country != representative.country {
		t.Fatal("last known-good real-artifact reader returned unexpected country semantics")
	}
}

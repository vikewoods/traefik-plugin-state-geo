package traefik_plugin_state_geo

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/oschwald/maxminddb-golang"
)

type Config struct {
	BlockedStates    []string `json:"blockedStates,omitempty"`
	WhitelistedIPs   []string `json:"whitelistedIPs,omitempty"`
	WhitelistedPaths []string `json:"whitelistedPaths,omitempty"`
	DBPath           string   `json:"dbPath,omitempty"`
	TemplatePath     string   `json:"templatePath,omitempty"`
	TemplateHTML     string   `json:"templateHTML,omitempty"`
	FailOpen         bool     `json:"failOpen,omitempty"`
	BlockNonUS       bool     `json:"blockNonUS,omitempty"`
	BlockUSStates    bool     `json:"blockUSStates,omitempty"`
}

func CreateConfig() *Config {
	return &Config{
		BlockedStates:    []string{},
		WhitelistedIPs:   []string{},
		WhitelistedPaths: []string{},
		DBPath:           "",
		TemplatePath:     "",
		TemplateHTML:     "",
		FailOpen:         true,
		BlockNonUS:       true,
		BlockUSStates:    true,
	}
}

type cacheEntry struct {
	allowed   bool
	stateCode string
}

type StateBlock struct {
	next             http.Handler
	blockedStates    map[string]struct{}
	whitelistedIPs   map[string]struct{}
	whitelistedPaths map[string]struct{}
	db               *maxminddb.Reader
	templatePath     string
	templateCache    string
	name             string
	cache            map[string]cacheEntry
	cacheMutex       sync.RWMutex
	blockNonUS       bool
	blockUSStates    bool

	// test-only hook
	mockGeoLookup func(net.IP) (countryCode string, subdivisionCode string, err error)
}

type geoRecord struct {
	Country struct {
		IsoCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		IsoCode string `maxminddb:"iso_code"`
	} `maxminddb:"subdivisions"`
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	var db *maxminddb.Reader

	if config.DBPath == "" {
		fmt.Fprintf(os.Stderr, "[%s] WARN: dbPath is empty; GeoIP database not loaded. Operating in fail-open pass-through mode.\n", name)
	} else {
		openedDB, err := maxminddb.Open(config.DBPath)
		if err != nil {
			if !config.FailOpen {
				return nil, fmt.Errorf("failed to open geoip database: %w", err)
			}
			fmt.Fprintf(os.Stderr, "[%s] WARN: failed to open geoip database at %s: %v. Operating in fail-open pass-through mode.\n", name, config.DBPath, err)
		} else {
			db = openedDB
		}
	}

	var templateContent string
	if config.TemplateHTML != "" {
		templateContent = config.TemplateHTML
	} else if config.TemplatePath != "" {
		content, err := os.ReadFile(config.TemplatePath)
		if err == nil {
			templateContent = string(content)
		} else {
			fmt.Fprintf(os.Stderr, "[%s] ERROR: failed to pre-load template: %v\n", name, err)
		}
	}

	blockedMap := make(map[string]struct{})
	for _, state := range config.BlockedStates {
		blockedMap[strings.ToUpper(strings.TrimSpace(state))] = struct{}{}
	}

	whitelistMap := make(map[string]struct{})
	for _, ip := range config.WhitelistedIPs {
		whitelistMap[strings.TrimSpace(ip)] = struct{}{}
	}

	whitelistedPathsMap := make(map[string]struct{})
	for _, path := range config.WhitelistedPaths {
		whitelistedPathsMap[path] = struct{}{}
	}

	return &StateBlock{
		blockedStates:    blockedMap,
		whitelistedIPs:   whitelistMap,
		whitelistedPaths: whitelistedPathsMap,
		db:               db,
		templatePath:     config.TemplatePath,
		templateCache:    templateContent,
		next:             next,
		name:             name,
		cache:            make(map[string]cacheEntry),
		blockNonUS:       config.BlockNonUS,
		blockUSStates:    config.BlockUSStates,
		mockGeoLookup:    nil,
	}, nil
}

func (a *StateBlock) isPathWhitelisted(reqPath string) bool {
	for whitelistedPath := range a.whitelistedPaths {
		if strings.HasPrefix(reqPath, whitelistedPath) {
			return true
		}
	}
	return false
}

func (a *StateBlock) serveBlocked(rw http.ResponseWriter, state string) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(http.StatusForbidden)

	fmt.Printf("[%s] DEBUG: Blocking request from state: %s\n", a.name, state)

	if a.templateCache != "" {
		html := strings.ReplaceAll(a.templateCache, "{{STATE}}", state)
		_, _ = rw.Write([]byte(html))
		return
	}

	_, _ = rw.Write([]byte(fmt.Sprintf("<h1>Access Denied</h1><p>State: %s</p>", state)))
}

func (a *StateBlock) resolveGeo(ip net.IP) (countryCode string, subdivisionCode string, err error) {
	if a.mockGeoLookup != nil {
		return a.mockGeoLookup(ip)
	}

	if a.db == nil {
		return "", "", nil
	}

	var record geoRecord
	if err := a.db.Lookup(ip, &record); err != nil {
		return "", "", err
	}

	countryCode = strings.ToUpper(strings.TrimSpace(record.Country.IsoCode))
	if len(record.Subdivisions) > 0 {
		subdivisionCode = strings.ToUpper(strings.TrimSpace(record.Subdivisions[0].IsoCode))
	}

	return countryCode, subdivisionCode, nil
}

func (a *StateBlock) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// 0. Check paths whitelist first
	if a.isPathWhitelisted(req.URL.Path) {
		fmt.Printf("[%s] DEBUG: Path %s is whitelisted, allowing\n", a.name, req.URL.Path)
		a.next.ServeHTTP(rw, req)
		return
	}

	ipStr := getRemoteIP(req)

	// 1. Check static IP whitelist
	if _, ok := a.whitelistedIPs[ipStr]; ok {
		fmt.Printf("[%s] DEBUG: IP %s is whitelisted, allowing\n", a.name, ipStr)
		a.next.ServeHTTP(rw, req)
		return
	}

	// 1.5 Fail-open if no runtime DB and no mock
	if a.db == nil && a.mockGeoLookup == nil {
		a.next.ServeHTTP(rw, req)
		return
	}

	// 2. Check Decision Cache
	a.cacheMutex.RLock()
	entry, found := a.cache[ipStr]
	a.cacheMutex.RUnlock()

	if found {
		if entry.allowed {
			fmt.Printf("[%s] DEBUG: Cache hit for %s: ALLOWED\n", a.name, ipStr)
			a.next.ServeHTTP(rw, req)
		} else {
			fmt.Printf("[%s] DEBUG: Cache hit for %s: BLOCKED (%s)\n", a.name, ipStr, entry.stateCode)
			a.serveBlocked(rw, entry.stateCode)
		}
		return
	}

	isAllowed := true
	stateCode := ""

	ip := net.ParseIP(ipStr)
	if ip != nil {
		if ip.IsPrivate() || ip.IsLoopback() {
			fmt.Printf("[%s] DEBUG: Private/loopback IP %s detected, allowing\n", a.name, ipStr)
			a.next.ServeHTTP(rw, req)
			return
		}

		countryCode, subdivisionCode, err := a.resolveGeo(ip)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] ERROR: GeoIP lookup failed for %s: %v\n", a.name, ipStr, err)
		} else {
			fmt.Printf("[%s] DEBUG: Geo lookup for %s -> country=%s subdivision=%s\n", a.name, ipStr, countryCode, subdivisionCode)

			if countryCode == "" {
				fmt.Printf("[%s] WARN: No country resolved for %s, allowing (fail-open unresolved lookup)\n", a.name, ipStr)
			} else if countryCode != "US" {
				if a.blockNonUS {
					isAllowed = false
					stateCode = countryCode
				}
			} else {
				if subdivisionCode != "" {
					stateCode = subdivisionCode
					if a.blockUSStates {
						if _, ok := a.blockedStates[stateCode]; ok {
							isAllowed = false
						}
					}
				} else {
					if a.blockUSStates {
						isAllowed = false
						stateCode = "Unknown"
					}
				}
			}
		}
	}

	// 4. Update Cache
	a.cacheMutex.Lock()
	if len(a.cache) < 1000 {
		a.cache[ipStr] = cacheEntry{allowed: isAllowed, stateCode: stateCode}
	}
	a.cacheMutex.Unlock()

	if !isAllowed {
		a.serveBlocked(rw, stateCode)
		return
	}

	fmt.Printf("[%s] DEBUG: New IP %s allowed (State: %s)\n", a.name, ipStr, stateCode)
	a.next.ServeHTTP(rw, req)
}

func getRemoteIP(req *http.Request) string {
	if cf := req.Header.Get("Cf-Connecting-Ip"); cf != "" {
		return strings.TrimSpace(cf)
	}

	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}

	res, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(req.RemoteAddr)
	}
	return strings.TrimSpace(res)
}

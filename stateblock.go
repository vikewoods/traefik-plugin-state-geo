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

	// VPN/Proxy/Tor Blocking
	EnableVPNBlocking bool   `json:"enableVPNBlocking,omitempty"`
	AnonDBPath        string `json:"anonDBPath,omitempty"`
	BlockVPN          bool   `json:"blockVPN,omitempty"`
	BlockProxy        bool   `json:"blockProxy,omitempty"`
	BlockHosting      bool   `json:"blockHosting,omitempty"`
	BlockTor          bool   `json:"blockTor,omitempty"`
}

func CreateConfig() *Config {
	return &Config{
		BlockedStates:  []string{},
		WhitelistedIPs: []string{},
		DBPath:         "/plugins-local/geoip.mmdb",
		TemplatePath:   "",
	}
}

type cacheEntry struct {
	allowed     bool
	stateCode   string
	blockReason string
}

type StateBlock struct {
	next             http.Handler
	blockedStates    map[string]struct{}
	whitelistedIPs   map[string]struct{}
	whitelistedPaths map[string]struct{}
	db               *maxminddb.Reader
	anonDB           *maxminddb.Reader
	enableVPNBlock   bool
	blockVPN         bool
	blockProxy       bool
	blockHosting     bool
	blockTor         bool
	templatePath     string
	templateCache    string
	name             string
	cache            map[string]cacheEntry
	cacheMutex       sync.RWMutex
}

type geoRecord struct {
	Country struct {
		IsoCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		IsoCode string `maxminddb:"iso_code"`
	} `maxminddb:"subdivisions"`
}

type anonRecord struct {
	IsAnonymous        bool `maxminddb:"is_anonymous"`
	IsAnonymousVPN     bool `maxminddb:"is_anonymous_vpn"`
	IsHostingProvider  bool `maxminddb:"is_hosting_provider"`
	IsPublicProxy      bool `maxminddb:"is_public_proxy"`
	IsTorExitNode      bool `maxminddb:"is_tor_exit_node"`
	IsResidentialProxy bool `maxminddb:"is_residential_proxy"`
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.DBPath == "" {
		return nil, fmt.Errorf("dbPath cannot be empty")
	}

	db, err := maxminddb.Open(config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open geoip database: %w", err)
	}

	var templateContent string
	if config.TemplatePath != "" {
		content, err := os.ReadFile(config.TemplatePath)
		if err == nil {
			templateContent = string(content)
		} else {
			fmt.Fprintf(os.Stderr, "[%s] ERROR: failed to pre-load template: %v\n", name, err)
		}
	}

	blockedMap := make(map[string]struct{})
	for _, state := range config.BlockedStates {
		blockedMap[strings.ToUpper(state)] = struct{}{}
	}

	whitelistMap := make(map[string]struct{})
	for _, ip := range config.WhitelistedIPs {
		whitelistMap[ip] = struct{}{}
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

func (a *StateBlock) checkAnonymousIP(ip net.IP) string {
	var record anonRecord
	err := a.anonDB.Lookup(ip, &record)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] ERROR: Anonymous IP lookup failed: %v\n", a.name, err)
		return ""
	}

	// Check in priority order
	if a.blockTor && record.IsTorExitNode {
		return "TOR"
	}
	if a.blockVPN && record.IsAnonymousVPN {
		return "VPN"
	}
	if a.blockProxy && (record.IsPublicProxy || record.IsResidentialProxy) {
		return "PROXY"
	}
	if a.blockHosting && record.IsHostingProvider {
		return "HOSTING"
	}

	return "" // Not blocked
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

func (a *StateBlock) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// 0. Check paths whitelist first
	if a.isPathWhitelisted(req.URL.Path) {
		fmt.Printf("[%s] DEBUG: Path %s is whitelisted, allowing\n", a.name, req.URL.Path)
		a.next.ServeHTTP(rw, req)
		return
	}

	ipStr := getRemoteIP(req)

	// 1. Check Whitelist first (Static)
	if _, ok := a.whitelistedIPs[ipStr]; ok {
		fmt.Printf("[%s] DEBUG: IP %s is whitelisted, allowing\n", a.name, ipStr)
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

	// 3. VPN/Proxy/Tor Check
	ip := net.ParseIP(ipStr)
	if ip != nil && a.anonDB != nil {
		blockReason := a.checkAnonymousIP(ip)
		if blockReason != "" {
			// Cache the VPN/Proxy block decision
			a.cacheMutex.Lock()
			if len(a.cache) < 1000 {
				a.cache[ipStr] = cacheEntry{
					allowed:     false,
					blockReason: blockReason,
				}
			}
			a.cacheMutex.Unlock()

			a.serveBlocked(rw, blockReason)
			return
		}
	}

	// 4. Database Lookup
	isAllowed := true
	stateCode := ""

	//ip := net.ParseIP(ipStr)
	if ip != nil {
		var record geoRecord
		err := a.db.Lookup(ip, &record)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] ERROR: GeoIP lookup failed for %s: %v\n", a.name, ipStr, err)
		} else {
			if record.Country.IsoCode != "US" {
				isAllowed = false
				stateCode = record.Country.IsoCode
			} else if len(record.Subdivisions) > 0 {
				stateCode = record.Subdivisions[0].IsoCode
				if _, ok := a.blockedStates[stateCode]; ok {
					isAllowed = false
				}
			} else {
				isAllowed = false
				stateCode = "Unknown"
			}
		}
	}

	// 5. Update Cache
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
	// Check CF-Connecting-Ip header first
	if cf := req.Header.Get("Cf-Connecting-Ip"); cf != "" {
		return strings.TrimSpace(cf)
	}

	// Check X-Forwarded-For if behind proxies
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		// Trim spaces
		return strings.TrimSpace(parts[0])
	}

	// Fallback to RemoteAddr
	res, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		// If SplitHostPort fails (e.g. no port), return raw RemoteAddr trimmed
		return strings.TrimSpace(req.RemoteAddr)
	}
	return strings.TrimSpace(res)
}

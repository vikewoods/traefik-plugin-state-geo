package traefik_plugin_state_geo

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Config defines the State Geo Block middleware configuration.
type Config struct {
	BlockedStates            []string `json:"blockedStates,omitempty"`
	WhitelistedIPs           []string `json:"whitelistedIPs,omitempty"`
	WhitelistedPaths         []string `json:"whitelistedPaths,omitempty"`
	WhitelistedPathPrefixes  []string `json:"whitelistedPathPrefixes,omitempty"`
	ClientIPHeaders          []string `json:"clientIPHeaders,omitempty"`
	TrustedProxyCIDRs        []string `json:"trustedProxyCIDRs,omitempty"`
	DBPath                   string   `json:"dbPath,omitempty"`
	DatabaseReloadInterval   string   `json:"databaseReloadInterval,omitempty"`
	CacheSize                int      `json:"cacheSize,omitempty"`
	CacheTTL                 string   `json:"cacheTTL,omitempty"`
	TemplatePath             string   `json:"templatePath,omitempty"`
	TemplateHTML             string   `json:"templateHTML,omitempty"`
	DatabaseFailurePolicy    string   `json:"databaseFailurePolicy,omitempty"`
	LookupFailurePolicy      string   `json:"lookupFailurePolicy,omitempty"`
	InvalidClientIPPolicy    string   `json:"invalidClientIPPolicy,omitempty"`
	UnknownCountryPolicy     string   `json:"unknownCountryPolicy,omitempty"`
	UnknownSubdivisionPolicy string   `json:"unknownSubdivisionPolicy,omitempty"`
	PrivateIPPolicy          string   `json:"privateIPPolicy,omitempty"`
	LogLevel                 string   `json:"logLevel,omitempty"`
	LogClientIP              bool     `json:"logClientIP,omitempty"`
	FailOpen                 bool     `json:"failOpen,omitempty"` // Deprecated: use DatabaseFailurePolicy.
	BlockNonUS               bool     `json:"blockNonUS,omitempty"`
	BlockUSStates            bool     `json:"blockUSStates,omitempty"`
}

// CreateConfig returns the middleware's documented default configuration.
func CreateConfig() *Config {
	return &Config{
		BlockedStates:           []string{},
		WhitelistedIPs:          []string{},
		WhitelistedPaths:        []string{},
		WhitelistedPathPrefixes: []string{},
		ClientIPHeaders: []string{
			"CF-Connecting-IP",
			"True-Client-IP",
			"X-Forwarded-For",
			"Forwarded",
			"X-Real-IP",
		},
		TrustedProxyCIDRs:        []string{},
		DBPath:                   "",
		DatabaseReloadInterval:   defaultDatabaseReloadInterval.String(),
		CacheSize:                defaultDecisionCacheSize,
		CacheTTL:                 defaultDecisionCacheTTL.String(),
		TemplatePath:             "",
		TemplateHTML:             "",
		DatabaseFailurePolicy:    "legacy",
		LookupFailurePolicy:      "allow",
		InvalidClientIPPolicy:    "allow",
		UnknownCountryPolicy:     "allow",
		UnknownSubdivisionPolicy: "deny",
		PrivateIPPolicy:          "allow",
		LogLevel:                 "warn",
		LogClientIP:              false,
		FailOpen:                 true,
		BlockNonUS:               true,
		BlockUSStates:            true,
	}
}

type cacheEntry struct {
	allowed   bool
	stateCode string
}

type stateBlock struct {
	next                    http.Handler
	blockedStates           map[string]struct{}
	whitelistedIPs          map[netip.Addr]struct{}
	whitelistedCIDRs        []netip.Prefix
	whitelistedPaths        map[string]struct{}
	whitelistedPathPrefixes []string
	dbManager               *databaseManager
	blockPage               *blockPageRenderer
	cache                   *decisionCache
	blockNonUS              bool
	blockUSStates           bool
	clientIPResolver        *clientIPResolver
	policies                policySet
	logger                  *pluginLogger

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

// New constructs a State Geo Block middleware instance.
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if next == nil {
		return nil, fmt.Errorf("next handler must not be nil")
	}

	logger, err := newPluginLogger(name, config.LogLevel, config.LogClientIP)
	if err != nil {
		return nil, fmt.Errorf("configure logging: %w", err)
	}

	policies, err := parsePolicySet(config)
	if err != nil {
		return nil, fmt.Errorf("configure decision policies: %w", err)
	}

	clientIPResolver, err := newClientIPResolver(config.ClientIPHeaders, config.TrustedProxyCIDRs)
	if err != nil {
		return nil, fmt.Errorf("configure client IP resolver: %w", err)
	}

	reloadInterval, err := parseDatabaseReloadInterval(config.DatabaseReloadInterval)
	if err != nil {
		return nil, fmt.Errorf("configure database reload: %w", err)
	}

	cacheSize, cacheTTL, err := parseDecisionCacheConfig(config.CacheSize, config.CacheTTL)
	if err != nil {
		return nil, fmt.Errorf("configure decision cache: %w", err)
	}
	cache := newDecisionCache(cacheSize, cacheTTL)

	blockPage, err := newBlockPageRenderer(config)
	if err != nil {
		return nil, fmt.Errorf("configure block page: %w", err)
	}

	blockedMap, err := parseBlockedStateRules(config.BlockedStates)
	if err != nil {
		return nil, err
	}

	whitelistMap, whitelistedCIDRs, err := parseWhitelistedIPRules(config.WhitelistedIPs)
	if err != nil {
		return nil, err
	}

	whitelistedPaths, whitelistedPathPrefixes, err := parseWhitelistedPathRules(
		config.WhitelistedPaths,
		config.WhitelistedPathPrefixes,
	)
	if err != nil {
		return nil, err
	}

	var dbManager *databaseManager

	if config.DBPath == "" {
		if policies.databaseFailure == databaseFailurePolicyError {
			return nil, fmt.Errorf("geoip database path is empty and databaseFailurePolicy requires an error")
		}
		logger.warn(
			ctx,
			"geoip database is not configured",
			"",
			slog.String("database_failure_policy", databaseFailurePolicyName(policies.databaseFailure)),
		)
	} else {
		dbManager, err = getSharedDatabaseManager(config.DBPath, reloadInterval)
		if err == nil {
			err = dbManager.ensureLoaded()
		}
		if err != nil {
			if policies.databaseFailure == databaseFailurePolicyError {
				return nil, fmt.Errorf("failed to open geoip database: %w", err)
			}
			logger.warn(
				ctx,
				"geoip database is unavailable",
				"",
				slog.String("path", config.DBPath),
				slog.Any("error", err),
				slog.String("database_failure_policy", databaseFailurePolicyName(policies.databaseFailure)),
			)
		}
	}

	return &stateBlock{
		blockedStates:           blockedMap,
		whitelistedIPs:          whitelistMap,
		whitelistedCIDRs:        whitelistedCIDRs,
		whitelistedPaths:        whitelistedPaths,
		whitelistedPathPrefixes: whitelistedPathPrefixes,
		dbManager:               dbManager,
		blockPage:               blockPage,
		next:                    next,
		cache:                   cache,
		blockNonUS:              config.BlockNonUS,
		blockUSStates:           config.BlockUSStates,
		clientIPResolver:        clientIPResolver,
		policies:                policies,
		logger:                  logger,
		mockGeoLookup:           nil,
	}, nil
}

func parseBlockedStateRules(entries []string) (map[string]struct{}, error) {
	states := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		state := strings.ToUpper(strings.TrimSpace(entry))
		if len(state) != 2 || state[0] < 'A' || state[0] > 'Z' || state[1] < 'A' || state[1] > 'Z' {
			return nil, fmt.Errorf("invalid blockedStates entry %q: expected a two-letter subdivision code", entry)
		}
		states[state] = struct{}{}
	}
	return states, nil
}

func parseWhitelistedIPRules(entries []string) (map[netip.Addr]struct{}, []netip.Prefix, error) {
	exactIPs := make(map[netip.Addr]struct{}, len(entries))
	cidrs := make([]netip.Prefix, 0, len(entries))
	seenCIDRs := make(map[string]struct{}, len(entries))

	for _, rawEntry := range entries {
		entry := strings.TrimSpace(rawEntry)
		if entry == "" {
			return nil, nil, fmt.Errorf("invalid whitelistedIPs entry %q: value must not be empty", rawEntry)
		}

		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid whitelistedIPs CIDR entry %q: %w", rawEntry, err)
			}
			prefix, err = normalizePrefix(prefix)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid whitelistedIPs CIDR entry %q: %w", rawEntry, err)
			}
			key := prefix.String()
			if _, exists := seenCIDRs[key]; exists {
				continue
			}
			seenCIDRs[key] = struct{}{}
			cidrs = append(cidrs, prefix)
			continue
		}

		ip, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid whitelistedIPs IP entry %q: %w", rawEntry, err)
		}
		ip, err = normalizeAddress(ip)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid whitelistedIPs IP entry %q: %w", rawEntry, err)
		}
		exactIPs[ip] = struct{}{}
	}

	return exactIPs, cidrs, nil
}

func (a *stateBlock) isPathWhitelisted(reqPath string) bool {
	normalized := normalizeRequestPath(reqPath)
	if _, exists := a.whitelistedPaths[normalized]; exists {
		return true
	}

	for _, prefix := range a.whitelistedPathPrefixes {
		if pathMatchesPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func (a *stateBlock) isIPWhitelisted(ipStr string) bool {
	ip, err := netip.ParseAddr(strings.TrimSpace(ipStr))
	if err != nil {
		return false
	}

	ip = ip.Unmap()
	if _, ok := a.whitelistedIPs[ip]; ok {
		return true
	}

	for _, cidr := range a.whitelistedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}

func (a *stateBlock) serveBlocked(
	rw http.ResponseWriter,
	req *http.Request,
	state string,
	clientIP string,
) {
	body, renderErr := a.blockPage.render(state)
	if renderErr != nil {
		a.logger.error(
			req.Context(),
			"block page rendering failed; built-in fallback used",
			clientIP,
			slog.Any("error", renderErr),
		)
	}

	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(http.StatusForbidden)

	a.logger.debug(
		req.Context(),
		"request denied by geo policy",
		clientIP,
		slog.String("state", state),
	)
	if _, err := rw.Write(body); err != nil {
		a.logger.error(req.Context(), "write block page response failed", clientIP, slog.Any("error", err))
	}
}

func (a *stateBlock) resolveGeo(
	ip net.IP,
	database geoDatabase,
) (countryCode string, subdivisionCode string, err error) {
	if a.mockGeoLookup != nil {
		return a.mockGeoLookup(ip)
	}

	if database == nil {
		return "", "", fmt.Errorf("geoip database is unavailable")
	}

	var record geoRecord
	if err := database.Lookup(ip, &record); err != nil {
		return "", "", err
	}

	countryCode = strings.ToUpper(strings.TrimSpace(record.Country.IsoCode))
	if len(record.Subdivisions) > 0 {
		subdivisionCode = strings.ToUpper(strings.TrimSpace(record.Subdivisions[0].IsoCode))
	}

	return countryCode, subdivisionCode, nil
}

func (a *stateBlock) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// A path bypass does not need a client address or database lookup.
	if a.isPathWhitelisted(req.URL.Path) {
		a.logger.debug(
			req.Context(),
			"request path is whitelisted",
			"",
			slog.String("path", req.URL.Path),
		)
		a.next.ServeHTTP(rw, req)
		return
	}

	clientIP, err := a.clientIPResolver.resolve(req)
	if err != nil {
		attrs := []slog.Attr{}
		remoteAddr := ""
		if a.logger.includeClientIP {
			remoteAddr = req.RemoteAddr
			attrs = append(attrs, slog.Any("error", err))
		}
		a.logger.error(req.Context(), "client ip resolution failed", remoteAddr, attrs...)
		a.servePolicyDecision(rw, req, a.policies.invalidClientIP, "")
		return
	}
	ipStr := clientIP.addr.String()

	// Apply the static IP/CIDR whitelist before GeoIP evaluation.
	if a.isIPWhitelisted(ipStr) {
		a.logger.debug(req.Context(), "client ip is whitelisted", ipStr)
		a.next.ServeHTTP(rw, req)
		return
	}

	snapshot := databaseSnapshot{}
	if a.mockGeoLookup == nil && a.dbManager != nil {
		loadedSnapshot, reloadErr := a.dbManager.snapshot()
		snapshot = loadedSnapshot
		if reloadErr != nil {
			a.logger.warn(
				req.Context(),
				"geoip database reload failed; last known-good reader retained",
				"",
				slog.Any("error", reloadErr),
			)
		}
	}

	// Apply the configured database-unavailable decision if there is no reader.
	if snapshot.reader == nil && a.mockGeoLookup == nil {
		policy := decisionPolicyAllow
		if a.policies.databaseFailure == databaseFailurePolicyDeny {
			policy = decisionPolicyDeny
		}
		a.servePolicyDecision(rw, req, policy, ipStr)
		return
	}

	// Check the bounded, expiring decision cache for this database generation.
	entry, found := a.cache.get(ipStr, snapshot.version)

	if found {
		if entry.allowed {
			a.logger.debug(req.Context(), "allowed decision cache hit", ipStr)
			a.next.ServeHTTP(rw, req)
		} else {
			a.logger.debug(
				req.Context(),
				"denied decision cache hit",
				ipStr,
				slog.String("state", entry.stateCode),
			)
			a.serveBlocked(rw, req, entry.stateCode, ipStr)
		}
		return
	}

	isAllowed := true
	stateCode := ""

	ip := net.IP(clientIP.addr.AsSlice())
	if ip.IsPrivate() || ip.IsLoopback() {
		switch a.policies.privateIP {
		case privateIPPolicyAllow:
			a.logger.debug(req.Context(), "private or loopback client ip allowed by policy", ipStr)
			a.next.ServeHTTP(rw, req)
			return
		case privateIPPolicyDeny:
			a.serveBlocked(rw, req, "Unknown", ipStr)
			return
		case privateIPPolicyLookup:
			// Continue to the same GeoIP lookup and policies used for public addresses.
		}
	}

	countryCode, subdivisionCode, err := a.resolveGeo(ip, snapshot.reader)
	if err != nil {
		attrs := []slog.Attr{}
		if a.logger.includeClientIP {
			attrs = append(attrs, slog.Any("error", err))
		}
		a.logger.error(req.Context(), "geoip lookup failed", ipStr, attrs...)
		a.servePolicyDecision(rw, req, a.policies.lookupFailure, ipStr)
		return
	}

	a.logger.debug(
		req.Context(),
		"geoip lookup completed",
		ipStr,
		slog.String("country", countryCode),
		slog.String("subdivision", subdivisionCode),
	)

	switch {
	case countryCode == "":
		isAllowed = a.policies.unknownCountry == decisionPolicyAllow
		if !isAllowed {
			stateCode = "Unknown"
		}
	case countryCode != "US":
		if a.blockNonUS {
			isAllowed = false
			stateCode = countryCode
		}
	case subdivisionCode != "":
		stateCode = subdivisionCode
		if a.blockUSStates {
			if _, isBlocked := a.blockedStates[stateCode]; isBlocked {
				isAllowed = false
			}
		}
	default:
		if a.blockUSStates {
			isAllowed = a.policies.unknownSubdivision == decisionPolicyAllow
			if !isAllowed {
				stateCode = "Unknown"
			}
		}
	}

	// A later database generation invalidates this cached decision.
	a.cache.set(
		ipStr,
		snapshot.version,
		cacheEntry{allowed: isAllowed, stateCode: stateCode},
	)

	if !isAllowed {
		a.serveBlocked(rw, req, stateCode, ipStr)
		return
	}

	a.logger.debug(
		req.Context(),
		"request allowed by geo policy",
		ipStr,
		slog.String("state", stateCode),
	)
	a.next.ServeHTTP(rw, req)
}

func (a *stateBlock) servePolicyDecision(
	rw http.ResponseWriter,
	req *http.Request,
	policy decisionPolicy,
	clientIP string,
) {
	if policy == decisionPolicyDeny {
		a.serveBlocked(rw, req, "Unknown", clientIP)
		return
	}

	a.next.ServeHTTP(rw, req)
}

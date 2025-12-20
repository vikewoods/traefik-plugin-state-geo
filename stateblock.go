package traefik_plugin_state_geo

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/oschwald/geoip2-golang/v2"
)

type Config struct {
	BlockedStates  []string `json:"blockedStates,omitempty"`
	WhitelistedIPs []string `json:"whitelistedIPs,omitempty"`
	DBPath         string   `json:"dbPath,omitempty"`
}

func CreateConfig() *Config {
	return &Config{
		BlockedStates:  []string{},
		WhitelistedIPs: []string{},
		DBPath:         "/plugins-local/geoip.mmdb",
	}
}

type StateBlock struct {
	next           http.Handler
	blockedStates  map[string]struct{}
	whitelistedIPs map[string]struct{}
	db             *geoip2.Reader
	name           string
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.DBPath == "" {
		return nil, fmt.Errorf("dbPath cannot be empty")
	}

	db, err := geoip2.Open(config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open geoip database: %w", err)
	}

	blockedMap := make(map[string]struct{})
	for _, state := range config.BlockedStates {
		blockedMap[strings.ToUpper(state)] = struct{}{}
	}

	whitelistMap := make(map[string]struct{})
	for _, ip := range config.WhitelistedIPs {
		whitelistMap[ip] = struct{}{}
	}

	return &StateBlock{
		blockedStates:  blockedMap,
		whitelistedIPs: whitelistMap,
		db:             db,
		next:           next,
		name:           name,
	}, nil
}

func (a *StateBlock) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ipStr := getRemoteIP(req)

	if _, ok := a.whitelistedIPs[ipStr]; ok {
		fmt.Printf("[%s] Whitelisted IP allowed: %s\n", a.name, ipStr)
		a.next.ServeHTTP(rw, req)
		return
	}

	ip, err := netip.ParseAddr(ipStr)

	if err == nil {
		record, err := a.db.City(ip)
		if err != nil {
			fmt.Printf("[%s] GeoIP error for IP %s: %v\n", a.name, ipStr, err)
		} else {
			// FIRST: Block everyone who is NOT from the US
			if record.Country.ISOCode != "US" {
				fmt.Printf("[%s] Blocked: Non-US traffic from %s (IP: %s)\n", a.name, record.Country.ISOCode, ipStr)
				rw.WriteHeader(http.StatusForbidden)
				_, _ = rw.Write([]byte("Access restricted to specific US states only."))
				return
			}

			// SECOND: If they ARE from US, check if they are in the blocked states list
			if len(record.Subdivisions) > 0 {
				stateCode := record.Subdivisions[0].ISOCode
				if _, ok := a.blockedStates[stateCode]; ok {
					fmt.Printf("[%s] Blocked: US State %s is in block-list (IP: %s)\n", a.name, stateCode, ipStr)
					rw.WriteHeader(http.StatusForbidden)
					_, _ = rw.Write([]byte(fmt.Sprintf("Access denied from state: %s", stateCode)))
					return
				}
				// If not blocked, allow through
				fmt.Printf("[%s] Allowed: US State %s (IP: %s)\n", a.name, stateCode, ipStr)
			} else {
				// Safety: US IP but no state data found
				fmt.Printf("[%s] Blocked: US IP %s with no state data\n", a.name, ipStr)
				rw.WriteHeader(http.StatusForbidden)
				return
			}
		}
	}

	a.next.ServeHTTP(rw, req)
}

func getRemoteIP(req *http.Request) string {
	// Check X-Forwarded-For if behind proxies
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// Fallback to RemoteAddr
	ip, _, _ := net.SplitHostPort(req.RemoteAddr)
	return ip
}

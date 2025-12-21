package traefik_plugin_state_geo

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/oschwald/geoip2-golang"
)

type Config struct {
	BlockedStates  []string `json:"blockedStates,omitempty"`
	WhitelistedIPs []string `json:"whitelistedIPs,omitempty"`
	DBPath         string   `json:"dbPath,omitempty"`
	TemplatePath   string   `json:"templatePath,omitempty"`
}

func CreateConfig() *Config {
	return &Config{
		BlockedStates:  []string{},
		WhitelistedIPs: []string{},
		DBPath:         "/plugins-local/geoip.mmdb",
		TemplatePath:   "",
	}
}

type StateBlock struct {
	next           http.Handler
	blockedStates  map[string]struct{}
	whitelistedIPs map[string]struct{}
	db             *geoip2.Reader
	templatePath   string
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
		templatePath:   config.TemplatePath,
		next:           next,
		name:           name,
	}, nil
}

func (a *StateBlock) serveBlocked(rw http.ResponseWriter, state string) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(http.StatusForbidden)

	if a.templatePath != "" {
		content, err := os.ReadFile(a.templatePath)
		if err == nil {
			html := strings.ReplaceAll(string(content), "{{STATE}}", state)
			_, _ = rw.Write([]byte(html))
			return
		}
		fmt.Printf("[%s] Error reading template file: %v\n", a.name, err)
	}

	_, _ = rw.Write([]byte(fmt.Sprintf("<h1>Access Denied</h1><p>State: %s</p>", state)))
}

func (a *StateBlock) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ipStr := getRemoteIP(req)

	if _, ok := a.whitelistedIPs[ipStr]; ok {
		fmt.Printf("[%s] Whitelisted IP allowed: %s\n", a.name, ipStr)
		a.next.ServeHTTP(rw, req)
		return
	}

	ip := net.ParseIP(ipStr)

	if ip != nil {
		record, err := a.db.City(ip)
		if err != nil {
			fmt.Printf("[%s] GeoIP error for IP %s: %v\n", a.name, ipStr, err)
		} else {
			// FIRST: Block everyone who is NOT from the US
			if record.Country.IsoCode != "US" {
				a.serveBlocked(rw, record.Country.IsoCode)
				return
			}

			// SECOND: If they ARE from US, check if they are in the blocked states list
			if len(record.Subdivisions) > 0 {
				stateCode := record.Subdivisions[0].IsoCode
				if _, ok := a.blockedStates[stateCode]; ok {
					a.serveBlocked(rw, stateCode)
					return
				}
			} else {
				a.serveBlocked(rw, "Unknown")
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

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
	BlockedStates []string `json:"blockedStates,omitempty"`
	DBPath        string   `json:"dbPath,omitempty"`
}

func CreateConfig() *Config {
	return &Config{
		BlockedStates: []string{},
		DBPath:        "/plugins-local/geoip.mmdb",
	}
}

type StateBlock struct {
	next          http.Handler
	blockedStates map[string]struct{}
	db            *geoip2.Reader
	name          string
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

	return &StateBlock{
		blockedStates: blockedMap,
		db:            db,
		next:          next,
		name:          name,
	}, nil
}

func (a *StateBlock) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ipStr := getRemoteIP(req)
	ip, err := netip.ParseAddr(ipStr)

	if err == nil {
		record, err := a.db.City(ip)
		if err == nil && len(record.Subdivisions) > 0 {
			stateCode := record.Subdivisions[0].ISOCode
			if _, ok := a.blockedStates[stateCode]; ok {
				rw.WriteHeader(http.StatusForbidden)
				_, _ = rw.Write([]byte(fmt.Sprintf("Access denied from state: %s", stateCode)))
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

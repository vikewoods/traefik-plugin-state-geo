package traefik_plugin_state_geo

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

var (
	benchmarkResolvedClientIP resolvedClientIP
	benchmarkCachedDecision   cacheEntry
	benchmarkGeoRecord        geoRecord
)

func BenchmarkClientIPResolver(b *testing.B) {
	resolver, err := newClientIPResolver(
		CreateConfig().ClientIPHeaders,
		[]string{"10.17.1.0/24"},
	)
	if err != nil {
		b.Fatalf("newClientIPResolver() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "10.17.1.12:443"
	req.Header.Set("CF-Connecting-IP", "2a06:98c0:3600::103")

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		benchmarkResolvedClientIP, err = resolver.resolve(req)
		if err != nil {
			b.Fatalf("resolve() error = %v", err)
		}
	}
}

func BenchmarkDecisionCacheHit(b *testing.B) {
	cache := newDecisionCache(defaultDecisionCacheSize, time.Minute)
	cache.set("2a06:98c0:3600::103", 1, cacheEntry{allowed: true})

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		var found bool
		benchmarkCachedDecision, found = cache.get("2a06:98c0:3600::103", 1)
		if !found {
			b.Fatal("expected cache hit")
		}
	}
}

func BenchmarkGeoLookup(b *testing.B) {
	reader, err := maxminddb.Open(testDatabasePath)
	if err != nil {
		b.Fatalf("maxminddb.Open() error = %v", err)
	}
	ip := net.ParseIP("2001:218::1")

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		benchmarkGeoRecord = geoRecord{}
		if err := reader.Lookup(ip, &benchmarkGeoRecord); err != nil {
			b.Fatalf("Lookup() error = %v", err)
		}
	}
}

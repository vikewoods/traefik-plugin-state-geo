package traefik_plugin_state_geo

import (
	"strings"
	"testing"
)

func FuzzParseAddress(f *testing.F) {
	for _, seed := range []string{
		"203.0.113.42",
		"203.0.113.42:443",
		"[2001:db8::1]:443",
		"::ffff:203.0.113.42",
		"not-an-ip",
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		addr, err := parseAddress(raw)
		if err != nil {
			return
		}
		if !addr.IsValid() || addr.Zone() != "" {
			t.Fatalf("parseAddress(%q) returned invalid address %q", raw, addr)
		}

		roundTrip, err := parseAddress(addr.String())
		if err != nil {
			t.Fatalf("parseAddress(%q) round-trip error = %v", addr, err)
		}
		if roundTrip != addr.Unmap() {
			t.Fatalf("parseAddress(%q) round-trip = %q, want %q", raw, roundTrip, addr.Unmap())
		}
	})
}

func FuzzParseXForwardedFor(f *testing.F) {
	for _, seed := range []string{
		"198.51.100.10, 10.17.1.20",
		"2001:db8::1, 2001:db8::2",
		"unknown",
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		chain, err := parseXForwardedFor([]string{raw})
		if err != nil {
			return
		}
		if len(chain) == 0 {
			t.Fatal("successful parse returned an empty chain")
		}
		for _, addr := range chain {
			if !addr.IsValid() || addr.Zone() != "" {
				t.Fatalf("successful parse returned invalid address %q", addr)
			}
		}
	})
}

func FuzzParseForwarded(f *testing.F) {
	for _, seed := range []string{
		`for=198.51.100.10;proto=https, for="[2001:db8::1]:443"`,
		"for=unknown",
		`for="unterminated`,
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		chain, err := parseForwarded([]string{raw})
		if err != nil {
			return
		}
		if len(chain) == 0 {
			t.Fatal("successful parse returned an empty chain")
		}
		for _, addr := range chain {
			if !addr.IsValid() || addr.Zone() != "" {
				t.Fatalf("successful parse returned invalid address %q", addr)
			}
		}
	})
}

func FuzzNormalizeWhitelistPath(f *testing.F) {
	for _, seed := range []struct {
		path     string
		isPrefix bool
	}{
		{path: "/health", isPrefix: false},
		{path: "/.well-known", isPrefix: true},
		{path: "/a/../admin", isPrefix: true},
		{path: "", isPrefix: false},
	} {
		f.Add(seed.path, seed.isPrefix)
	}

	f.Fuzz(func(t *testing.T, raw string, isPrefix bool) {
		normalized, err := normalizeWhitelistPath(raw, isPrefix)
		if err != nil {
			return
		}
		if !strings.HasPrefix(normalized, "/") {
			t.Fatalf("normalized path %q does not start with a slash", normalized)
		}
		if strings.ContainsAny(normalized, "?#") {
			t.Fatalf("normalized path %q contains a query or fragment", normalized)
		}
		if isPrefix && normalized == "/" {
			t.Fatal("root prefix must not be accepted")
		}
	})
}

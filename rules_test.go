package traefik_plugin_state_geo

import (
	"net/netip"
	"testing"
)

func TestBlockedStateRulesNormalizeAndDeduplicate(t *testing.T) {
	states, err := parseBlockedStateRules([]string{" ca ", "NY", "CA"})
	if err != nil {
		t.Fatalf("parseBlockedStateRules() error = %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("blocked states = %v, want two normalized entries", states)
	}
	if _, exists := states["CA"]; !exists {
		t.Fatal("normalized CA state is missing")
	}
	if _, exists := states["NY"]; !exists {
		t.Fatal("NY state is missing")
	}
}

func TestBlockedStateRulesRejectInvalidCodes(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{name: "empty", entry: ""},
		{name: "too short", entry: "C"},
		{name: "too long", entry: "CAL"},
		{name: "non-letter", entry: "C1"},
		{name: "unicode", entry: "🇺🇸"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseBlockedStateRules([]string{test.entry}); err == nil {
				t.Fatal("parseBlockedStateRules() error = nil, want validation error")
			}
		})
	}
}

func TestWhitelistedIPRulesNormalizeAndDeduplicate(t *testing.T) {
	exact, prefixes, err := parseWhitelistedIPRules([]string{
		"::ffff:203.0.113.25",
		"203.0.113.25",
		"::ffff:198.51.100.0/120",
		"198.51.100.0/24",
	})
	if err != nil {
		t.Fatalf("parseWhitelistedIPRules() error = %v", err)
	}

	wantedIP := netip.MustParseAddr("203.0.113.25")
	if len(exact) != 1 {
		t.Fatalf("exact IP rules = %v, want one normalized entry", exact)
	}
	if _, exists := exact[wantedIP]; !exists {
		t.Fatalf("normalized exact IP %s is missing", wantedIP)
	}

	wantedPrefix := netip.MustParsePrefix("198.51.100.0/24")
	if len(prefixes) != 1 || prefixes[0] != wantedPrefix {
		t.Fatalf("CIDR rules = %v, want [%s]", prefixes, wantedPrefix)
	}
}

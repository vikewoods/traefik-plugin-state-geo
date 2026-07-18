package traefik_plugin_state_geo

import "testing"

func TestWhitelistedPathRuleMatching(t *testing.T) {
	exact, prefixes, err := parseWhitelistedPathRules(
		[]string{"/health", "/"},
		[]string{"/.well-known/", "/api/public"},
	)
	if err != nil {
		t.Fatalf("parseWhitelistedPathRules() error = %v", err)
	}

	matches := func(requestPath string) bool {
		normalized := normalizeRequestPath(requestPath)
		if _, exists := exact[normalized]; exists {
			return true
		}
		for _, prefix := range prefixes {
			if pathMatchesPrefix(normalized, prefix) {
				return true
			}
		}
		return false
	}

	tests := []struct {
		name        string
		requestPath string
		expected    bool
	}{
		{name: "exact health path", requestPath: "/health", expected: true},
		{name: "exact path does not match suffix", requestPath: "/healthz", expected: false},
		{name: "exact path does not match descendant", requestPath: "/health/ready", expected: false},
		{name: "exact root path", requestPath: "/", expected: true},
		{name: "prefix root itself", requestPath: "/.well-known", expected: true},
		{name: "prefix descendant", requestPath: "/.well-known/acme-challenge/token", expected: true},
		{name: "prefix does not match similar segment", requestPath: "/.well-known-attack", expected: false},
		{name: "public api descendant", requestPath: "/api/public/status", expected: true},
		{name: "normalized traversal cannot retain prefix", requestPath: "/.well-known/../admin", expected: false},
		{name: "normalized double slash remains segment safe", requestPath: "/api//public/status", expected: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := matches(test.requestPath); actual != test.expected {
				t.Errorf("match(%q) = %t, want %t", test.requestPath, actual, test.expected)
			}
		})
	}
}

func TestWhitelistedPathRulesRejectUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		exact    []string
		prefixes []string
	}{
		{name: "empty exact path", exact: []string{""}},
		{name: "whitespace exact path", exact: []string{"  "}},
		{name: "relative exact path", exact: []string{"health"}},
		{name: "exact path with query", exact: []string{"/health?full=true"}},
		{name: "exact path with fragment", exact: []string{"/health#details"}},
		{name: "exact path with control character", exact: []string{"/health\nadmin"}},
		{name: "exact path with trailing slash", exact: []string{"/health/"}},
		{name: "exact path with traversal", exact: []string{"/public/../admin"}},
		{name: "exact path with duplicate separators", exact: []string{"/api//public"}},
		{name: "root prefix", prefixes: []string{"/"}},
		{name: "root prefix with trailing slash", prefixes: []string{"//"}},
		{name: "relative prefix", prefixes: []string{"api/public"}},
		{name: "prefix with traversal", prefixes: []string{"/public/../admin"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := parseWhitelistedPathRules(test.exact, test.prefixes); err == nil {
				t.Fatal("parseWhitelistedPathRules() error = nil, want validation error")
			}
		})
	}
}

func TestWhitelistedPathPrefixDuplicatesAreRemoved(t *testing.T) {
	_, prefixes, err := parseWhitelistedPathRules(nil, []string{"/api", "/api/", "/api"})
	if err != nil {
		t.Fatalf("parseWhitelistedPathRules() error = %v", err)
	}
	if len(prefixes) != 1 || prefixes[0] != "/api" {
		t.Fatalf("prefixes = %v, want [/api]", prefixes)
	}
}

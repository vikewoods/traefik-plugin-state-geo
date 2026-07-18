package traefik_plugin_state_geo

import (
	"fmt"
	pathpkg "path"
	"strings"
)

func parseWhitelistedPathRules(
	exactEntries,
	prefixEntries []string,
) (map[string]struct{}, []string, error) {
	exactPaths := make(map[string]struct{}, len(exactEntries))
	for _, entry := range exactEntries {
		normalized, err := normalizeWhitelistPath(entry, false)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid whitelistedPaths entry %q: %w", entry, err)
		}
		exactPaths[normalized] = struct{}{}
	}

	prefixes := make([]string, 0, len(prefixEntries))
	seenPrefixes := make(map[string]struct{}, len(prefixEntries))
	for _, entry := range prefixEntries {
		normalized, err := normalizeWhitelistPath(entry, true)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid whitelistedPathPrefixes entry %q: %w", entry, err)
		}
		if _, exists := seenPrefixes[normalized]; exists {
			continue
		}
		seenPrefixes[normalized] = struct{}{}
		prefixes = append(prefixes, normalized)
	}

	return exactPaths, prefixes, nil
}

func normalizeWhitelistPath(rawPath string, isPrefix bool) (string, error) {
	value := strings.TrimSpace(rawPath)
	if value == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("path must start with a slash")
	}
	if strings.ContainsAny(value, "?#") {
		return "", fmt.Errorf("query strings and fragments are not allowed")
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return "", fmt.Errorf("control characters are not allowed")
		}
	}

	if !isPrefix && value != "/" && strings.HasSuffix(value, "/") {
		return "", fmt.Errorf("exact path must not end with a slash")
	}
	if isPrefix {
		value = strings.TrimSuffix(value, "/")
		if value == "" {
			return "", fmt.Errorf("root prefix would whitelist every request")
		}
	}

	normalized := pathpkg.Clean(value)
	if normalized != value {
		return "", fmt.Errorf("path must be normalized as %q", normalized)
	}
	if isPrefix && normalized == "/" {
		return "", fmt.Errorf("root prefix would whitelist every request")
	}

	return normalized, nil
}

func pathMatchesPrefix(requestPath, prefix string) bool {
	return requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/")
}

func normalizeRequestPath(requestPath string) string {
	return pathpkg.Clean(requestPath)
}

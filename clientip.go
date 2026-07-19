package traefik_plugin_state_geo

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
)

const remoteAddrSource = "RemoteAddr"

type clientIPHeaderKind uint8

const (
	clientIPHeaderSingle clientIPHeaderKind = iota + 1
	clientIPHeaderXForwardedFor
	clientIPHeaderForwarded
)

type clientIPHeader struct {
	name string
	kind clientIPHeaderKind
}

type resolvedClientIP struct {
	addr   netip.Addr
	source string
}

type clientIPResolver struct {
	headers                     []clientIPHeader
	trustedProxies              []netip.Prefix
	rejectInvalidTrustedHeaders bool
	logger                      *pluginLogger
}

func newClientIPResolver(
	headerNames, trustedProxyCIDRs []string,
	rejectInvalidTrustedHeaders bool,
) (*clientIPResolver, error) {
	headers := make([]clientIPHeader, 0, len(headerNames))
	seenHeaders := make(map[string]struct{}, len(headerNames))

	for _, rawName := range headerNames {
		name := strings.TrimSpace(rawName)
		if !isValidHeaderName(name) {
			return nil, fmt.Errorf("invalid client IP header name %q", rawName)
		}

		name = http.CanonicalHeaderKey(name)
		key := strings.ToLower(name)
		if _, exists := seenHeaders[key]; exists {
			continue
		}
		seenHeaders[key] = struct{}{}

		kind := clientIPHeaderSingle
		switch key {
		case "x-forwarded-for":
			kind = clientIPHeaderXForwardedFor
		case "forwarded":
			kind = clientIPHeaderForwarded
		}

		headers = append(headers, clientIPHeader{
			name: name,
			kind: kind,
		})
	}

	trustedProxies := make([]netip.Prefix, 0, len(trustedProxyCIDRs))
	for _, rawEntry := range trustedProxyCIDRs {
		prefix, err := parseTrustedProxyPrefix(rawEntry)
		if err != nil {
			return nil, err
		}
		trustedProxies = append(trustedProxies, prefix)
	}

	return &clientIPResolver{
		headers:                     headers,
		trustedProxies:              trustedProxies,
		rejectInvalidTrustedHeaders: rejectInvalidTrustedHeaders,
	}, nil
}

func (r *clientIPResolver) resolve(req *http.Request) (resolvedClientIP, error) {
	if req == nil {
		return resolvedClientIP{}, fmt.Errorf("request is nil")
	}

	peer, err := parseAddress(req.RemoteAddr)
	if err != nil {
		return resolvedClientIP{}, fmt.Errorf("parsing RemoteAddr %q: %w", req.RemoteAddr, err)
	}

	direct := resolvedClientIP{
		addr:   peer,
		source: remoteAddrSource,
	}
	if !r.isTrustedProxy(peer) {
		return direct, nil
	}

	for _, header := range r.headers {
		values := req.Header.Values(header.name)
		if len(values) == 0 {
			continue
		}

		addr, err := r.resolveHeader(header, values)
		if err != nil {
			r.logger.warn(
				req.Context(),
				"trusted client ip header is invalid",
				"",
				slog.String("header", header.name),
				slog.Bool("request_rejected", r.rejectInvalidTrustedHeaders),
			)
			if r.rejectInvalidTrustedHeaders {
				return resolvedClientIP{}, fmt.Errorf(
					"trusted client IP header %q is invalid",
					header.name,
				)
			}
			continue
		}

		return resolvedClientIP{
			addr:   addr,
			source: header.name,
		}, nil
	}

	return direct, nil
}

func (r *clientIPResolver) resolveHeader(header clientIPHeader, values []string) (netip.Addr, error) {
	switch header.kind {
	case clientIPHeaderXForwardedFor:
		chain, err := parseXForwardedFor(values)
		if err != nil {
			return netip.Addr{}, err
		}
		return r.firstUntrustedFromRight(chain), nil
	case clientIPHeaderForwarded:
		chain, err := parseForwarded(values)
		if err != nil {
			return netip.Addr{}, err
		}
		return r.firstUntrustedFromRight(chain), nil
	case clientIPHeaderSingle:
		return parseSingleIPHeader(values)
	default:
		return netip.Addr{}, fmt.Errorf("unsupported client IP header kind %d", header.kind)
	}
}

func (r *clientIPResolver) firstUntrustedFromRight(chain []netip.Addr) netip.Addr {
	for index := len(chain) - 1; index >= 0; index-- {
		if !r.isTrustedProxy(chain[index]) {
			return chain[index]
		}
	}

	return chain[0]
}

func (r *clientIPResolver) isTrustedProxy(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range r.trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}

func parseTrustedProxyPrefix(rawEntry string) (netip.Prefix, error) {
	entry := strings.TrimSpace(rawEntry)
	if entry == "" {
		return netip.Prefix{}, fmt.Errorf("trusted proxy entry must not be empty")
	}

	if strings.Contains(entry, "/") {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid trusted proxy CIDR %q: %w", rawEntry, err)
		}
		return normalizePrefix(prefix)
	}

	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid trusted proxy address %q: %w", rawEntry, err)
	}
	addr, err = normalizeAddress(addr)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid trusted proxy address %q: %w", rawEntry, err)
	}

	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func normalizePrefix(prefix netip.Prefix) (netip.Prefix, error) {
	addr := prefix.Addr()
	if addr.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("scoped address zones are not supported")
	}

	if !addr.Is4In6() {
		return prefix.Masked(), nil
	}

	bits := prefix.Bits() - 96
	if bits < 0 {
		return netip.Prefix{}, fmt.Errorf("IPv4-mapped IPv6 prefix %q is broader than /96", prefix)
	}

	return netip.PrefixFrom(addr.Unmap(), bits).Masked(), nil
}

func parseSingleIPHeader(values []string) (netip.Addr, error) {
	if len(values) != 1 {
		return netip.Addr{}, fmt.Errorf("single-IP header has %d values", len(values))
	}

	value := strings.TrimSpace(values[0])
	if value == "" || strings.Contains(value, ",") {
		return netip.Addr{}, fmt.Errorf("single-IP header value %q is empty or ambiguous", values[0])
	}

	return parseAddress(value)
}

func parseXForwardedFor(values []string) ([]netip.Addr, error) {
	chain := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		parts := strings.Split(value, ",")
		for _, part := range parts {
			addr, err := parseAddress(part)
			if err != nil {
				return nil, fmt.Errorf("invalid X-Forwarded-For value %q: %w", part, err)
			}
			chain = append(chain, addr)
		}
	}

	if len(chain) == 0 {
		return nil, fmt.Errorf("X-Forwarded-For has no addresses")
	}

	return chain, nil
}

func parseForwarded(values []string) ([]netip.Addr, error) {
	chain := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		elements, err := splitHeaderValue(value, ',')
		if err != nil {
			return nil, fmt.Errorf("invalid Forwarded header: %w", err)
		}

		for _, element := range elements {
			params, err := splitHeaderValue(element, ';')
			if err != nil {
				return nil, fmt.Errorf("invalid Forwarded element %q: %w", element, err)
			}

			foundFor := false
			for _, param := range params {
				name, value, ok := strings.Cut(param, "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(name), "for") {
					continue
				}
				if foundFor {
					return nil, fmt.Errorf("forwarded element %q contains multiple for parameters", element)
				}

				value, err = unquoteHeaderValue(strings.TrimSpace(value))
				if err != nil {
					return nil, fmt.Errorf("invalid Forwarded for value: %w", err)
				}
				addr, err := parseAddress(value)
				if err != nil {
					return nil, fmt.Errorf("invalid Forwarded for value %q: %w", value, err)
				}

				chain = append(chain, addr)
				foundFor = true
			}
		}
	}

	if len(chain) == 0 {
		return nil, fmt.Errorf("forwarded header has no usable for parameters")
	}

	return chain, nil
}

func splitHeaderValue(value string, separator byte) ([]string, error) {
	parts := make([]string, 0, 1)
	start := 0
	inQuotes := false
	escaped := false

	for index := 0; index < len(value); index++ {
		character := value[index]
		if escaped {
			escaped = false
			continue
		}
		if inQuotes && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			inQuotes = !inQuotes
			continue
		}
		if character != separator || inQuotes {
			continue
		}

		part := strings.TrimSpace(value[start:index])
		if part == "" {
			return nil, fmt.Errorf("empty list element")
		}
		parts = append(parts, part)
		start = index + 1
	}

	if inQuotes || escaped {
		return nil, fmt.Errorf("unterminated quoted string")
	}

	part := strings.TrimSpace(value[start:])
	if part == "" {
		return nil, fmt.Errorf("empty list element")
	}
	parts = append(parts, part)

	return parts, nil
}

func unquoteHeaderValue(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("value is empty")
	}
	if value[0] != '"' {
		if strings.Contains(value, "\"") {
			return "", fmt.Errorf("unexpected quote in %q", value)
		}
		return value, nil
	}
	if len(value) < 2 || value[len(value)-1] != '"' {
		return "", fmt.Errorf("unterminated quoted value %q", value)
	}

	var result strings.Builder
	result.Grow(len(value) - 2)
	for index := 1; index < len(value)-1; index++ {
		character := value[index]
		if character == '"' {
			return "", fmt.Errorf("unexpected quote in %q", value)
		}
		if character != '\\' {
			result.WriteByte(character)
			continue
		}

		index++
		if index >= len(value)-1 {
			return "", fmt.Errorf("unterminated escape in %q", value)
		}
		result.WriteByte(value[index])
	}

	return result.String(), nil
}

func parseAddress(rawAddress string) (netip.Addr, error) {
	value := strings.TrimSpace(rawAddress)
	if value == "" {
		return netip.Addr{}, fmt.Errorf("address is empty")
	}

	if addr, err := netip.ParseAddr(value); err == nil {
		return normalizeAddress(addr)
	}

	if addrPort, err := netip.ParseAddrPort(value); err == nil {
		return normalizeAddress(addrPort.Addr())
	}

	if len(value) > 2 && value[0] == '[' && value[len(value)-1] == ']' {
		addr, err := netip.ParseAddr(value[1 : len(value)-1])
		if err == nil {
			return normalizeAddress(addr)
		}
	}

	return netip.Addr{}, fmt.Errorf("%q is not an IP address", rawAddress)
}

func normalizeAddress(addr netip.Addr) (netip.Addr, error) {
	if !addr.IsValid() {
		return netip.Addr{}, fmt.Errorf("address is invalid")
	}
	if addr.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("scoped address zones are not supported")
	}

	return addr.Unmap(), nil
}

func isValidHeaderName(name string) bool {
	if name == "" {
		return false
	}

	for index := 0; index < len(name); index++ {
		character := name[index]
		isAlphaNumeric := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9'
		if isAlphaNumeric || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			continue
		}
		return false
	}

	return true
}

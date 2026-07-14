package clientip

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func mustPrefixes(t *testing.T, values ...string) []netip.Prefix {
	t.Helper()
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			t.Fatalf("parse prefix %q: %v", value, err)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

func newRequest(remoteAddr string, forwardedFor ...string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/query/login", nil)
	req.RemoteAddr = remoteAddr
	for _, value := range forwardedFor {
		req.Header.Add("X-Forwarded-For", value)
	}
	return req
}

func TestResolveNoTrustedProxiesIgnoresForwardedFor(t *testing.T) {
	resolver := NewResolver(nil)
	result := resolver.Resolve(newRequest("203.0.113.7:50000", "198.51.100.9"))
	if !result.Valid || result.Source != SourceRemote || result.Key() != "203.0.113.7" {
		t.Fatalf("expected remote 203.0.113.7, got %+v key=%q", result, result.Key())
	}
}

func TestResolveUntrustedPeerIgnoresForwardedFor(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32"))
	result := resolver.Resolve(newRequest("203.0.113.7:50000", "198.51.100.9"))
	if result.Source != SourceRemote || result.Key() != "203.0.113.7" {
		t.Fatalf("forwarded header must be ignored for untrusted peer, got %+v key=%q", result, result.Key())
	}
}

func TestResolveSingleTrustedLoopbackProxy(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32"))
	result := resolver.Resolve(newRequest("127.0.0.1:50000", "203.0.113.10"))
	if result.Source != SourceForwarded || result.Key() != "203.0.113.10" {
		t.Fatalf("expected forwarded 203.0.113.10, got %+v key=%q", result, result.Key())
	}
}

func TestResolveTwoTrustedHops(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32", "10.0.0.0/8"))
	result := resolver.Resolve(newRequest("127.0.0.1:50000", "203.0.113.10, 10.1.2.3"))
	if result.Key() != "203.0.113.10" {
		t.Fatalf("expected client behind two trusted hops, got %q", result.Key())
	}
}

func TestResolveMultiHopStripsTrustedRightToLeft(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32", "10.0.0.0/8"))
	result := resolver.Resolve(newRequest("127.0.0.1:50000", "198.51.100.9, 203.0.113.10, 10.9.9.9, 10.1.2.3"))
	if result.Key() != "203.0.113.10" {
		t.Fatalf("expected first untrusted from the right, got %q", result.Key())
	}
}

func TestResolveLeftSpoofDoesNotWin(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32"))
	result := resolver.Resolve(newRequest("127.0.0.1:50000", "1.2.3.4, 203.0.113.10"))
	if result.Key() != "203.0.113.10" {
		t.Fatalf("leftmost spoofed address must not be selected, got %q", result.Key())
	}
}

func TestResolveAllHopsTrustedReturnsLeftmost(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32", "10.0.0.0/8"))
	result := resolver.Resolve(newRequest("127.0.0.1:50000", "10.5.5.5, 10.1.2.3"))
	if result.Key() != "10.5.5.5" {
		t.Fatalf("expected leftmost address of an all-trusted chain, got %q", result.Key())
	}
}

func TestResolveTrustedPeerWithoutForwardedFor(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32"))
	result := resolver.Resolve(newRequest("127.0.0.1:50000"))
	if result.Source != SourceRemote || result.Key() != "127.0.0.1" {
		t.Fatalf("expected trusted peer itself, got %+v key=%q", result, result.Key())
	}
}

func TestResolveMultipleForwardedForHeaderValues(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32", "10.0.0.0/8"))
	result := resolver.Resolve(newRequest("127.0.0.1:50000", "203.0.113.10", "10.1.2.3"))
	if result.Key() != "203.0.113.10" {
		t.Fatalf("multiple header values must merge per HTTP semantics, got %q", result.Key())
	}
}

func TestResolveInvalidChainFallsBackToPeer(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32"))
	cases := map[string]string{
		"empty item":     "203.0.113.10,, 198.51.100.9",
		"invalid ip":     "not-an-ip",
		"unknown":        "unknown, 203.0.113.10",
		"port entry":     "203.0.113.10:443",
		"hostname":       "evil.example.com",
		"zoned address":  "fe80::1%eth0",
		"trailing comma": "203.0.113.10,",
	}
	for name, xff := range cases {
		result := resolver.Resolve(newRequest("127.0.0.1:50000", xff))
		if result.Source != SourceRemote || result.Key() != "127.0.0.1" {
			t.Fatalf("%s: expected fallback to peer, got %+v key=%q", name, result, result.Key())
		}
	}
}

func TestResolveIPv4AndIPv6Chains(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "::1/128", "127.0.0.1/32"))

	result := resolver.Resolve(newRequest("[::1]:50000", "2001:db8::5"))
	if result.Key() != "2001:db8::5" {
		t.Fatalf("expected IPv6 client, got %q", result.Key())
	}

	result = resolver.Resolve(newRequest("127.0.0.1:50000", "198.51.100.9"))
	if result.Key() != "198.51.100.9" {
		t.Fatalf("expected IPv4 client, got %q", result.Key())
	}
}

func TestResolveIPv4MappedIPv6IsUnmapped(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32"))
	result := resolver.Resolve(newRequest("127.0.0.1:50000", "::ffff:203.0.113.10"))
	if result.Key() != "203.0.113.10" {
		t.Fatalf("IPv4-mapped IPv6 must unmap to plain IPv4, got %q", result.Key())
	}

	result = resolver.Resolve(newRequest("::ffff:198.51.100.9"))
	if result.Key() != "198.51.100.9" {
		t.Fatalf("IPv4-mapped peer must unmap, got %q", result.Key())
	}
}

func TestResolvePeerAddressFormats(t *testing.T) {
	resolver := NewResolver(nil)

	if key := resolver.Resolve(newRequest("198.51.100.9:50000")).Key(); key != "198.51.100.9" {
		t.Fatalf("IPv4 host:port, got %q", key)
	}
	if key := resolver.Resolve(newRequest("[2001:db8::5]:50000")).Key(); key != "2001:db8::5" {
		t.Fatalf("IPv6 host:port, got %q", key)
	}
	if key := resolver.Resolve(newRequest("198.51.100.9")).Key(); key != "198.51.100.9" {
		t.Fatalf("bare IPv4 peer, got %q", key)
	}
	if key := resolver.Resolve(newRequest("2001:db8::5")).Key(); key != "2001:db8::5" {
		t.Fatalf("bare IPv6 peer, got %q", key)
	}
}

func TestResolveInvalidPeerUsesUnknownBucket(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32"))
	for _, remoteAddr := range []string{"", "not-an-address", "evil.example.com:50000", "fe80::1%eth0"} {
		result := resolver.Resolve(newRequest(remoteAddr, "203.0.113.10"))
		if result.Valid || result.Source != SourceUnknown {
			t.Fatalf("remote %q: expected invalid result, got %+v", remoteAddr, result)
		}
		if key := result.Key(); key != UnknownClientKey {
			t.Fatalf("remote %q: expected fixed unknown bucket, got %q", remoteAddr, key)
		}
	}
}

func TestResolveChainSizeLimits(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32"))

	// Exactly 32 entries is allowed.
	entries := make([]string, maxForwardedForEntries)
	for i := range entries {
		entries[i] = "203.0.113.10"
	}
	result := resolver.Resolve(newRequest("127.0.0.1:50000", strings.Join(entries, ", ")))
	if result.Source != SourceForwarded || result.Key() != "203.0.113.10" {
		t.Fatalf("chain of exactly %d entries must parse, got %+v", maxForwardedForEntries, result)
	}

	// One more entry invalidates the whole chain.
	tooMany := strings.Join(append(entries, "203.0.113.10"), ", ")
	result = resolver.Resolve(newRequest("127.0.0.1:50000", tooMany))
	if result.Source != SourceRemote || result.Key() != "127.0.0.1" {
		t.Fatalf("chain over the entry limit must fall back to peer, got %+v", result)
	}

	// At most 4096 merged bytes; a valid address padded with spaces up to the
	// boundary still parses, one byte over falls back.
	padded := "203.0.113.10" + strings.Repeat(" ", maxForwardedForBytes-len("203.0.113.10"))
	if len(padded) != maxForwardedForBytes {
		t.Fatalf("test setup: padded length %d", len(padded))
	}
	result = resolver.Resolve(newRequest("127.0.0.1:50000", padded))
	if result.Key() != "203.0.113.10" {
		t.Fatalf("chain at the byte boundary must parse, got %q", result.Key())
	}
	result = resolver.Resolve(newRequest("127.0.0.1:50000", padded+" "))
	if result.Source != SourceRemote || result.Key() != "127.0.0.1" {
		t.Fatalf("chain over the byte limit must fall back to peer, got %+v", result)
	}
}

func TestResultKeyNeverEchoesRawInput(t *testing.T) {
	resolver := NewResolver(mustPrefixes(t, "127.0.0.1/32"))
	malformed := "<script>alert(1)</script>"
	result := resolver.Resolve(newRequest(malformed, malformed))
	if key := result.Key(); key != UnknownClientKey || strings.Contains(key, "<") {
		t.Fatalf("key must be the fixed bucket, got %q", key)
	}
}

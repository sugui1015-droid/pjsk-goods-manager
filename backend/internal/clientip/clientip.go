// Package clientip resolves the real client address for rate limiting and
// abuse tracking. By default no proxy is trusted and only the TCP peer
// address (RemoteAddr) is used; X-Forwarded-For is honored only when the
// direct peer is inside an explicitly configured trusted CIDR, walking the
// chain right-to-left so a client cannot smuggle a fake address on the left.
package clientip

import (
	"net/http"
	"net/netip"
	"strings"
)

// Abuse limits for the merged X-Forwarded-For value. A chain exceeding
// either limit is treated as invalid as a whole and resolution falls back
// to the TCP peer address.
const (
	maxForwardedForBytes   = 4096
	maxForwardedForEntries = 32
)

// UnknownClientKey is the fixed rate-limit key shared by every request whose
// peer address cannot be parsed. Collapsing all malformed sources into one
// bucket is deliberate: they share limits instead of bypassing them.
const UnknownClientKey = "unknown-client"

type Source int

const (
	// SourceUnknown means RemoteAddr itself was unparseable.
	SourceUnknown Source = iota
	// SourceRemote means the TCP peer address was used directly.
	SourceRemote
	// SourceForwarded means the address came from a validated X-Forwarded-For chain.
	SourceForwarded
)

type Result struct {
	Addr   netip.Addr
	Source Source
	Valid  bool
}

// Key returns the canonical rate-limit key for this result. It never returns
// an empty string or any raw unparsed input.
func (r Result) Key() string {
	if !r.Valid {
		return UnknownClientKey
	}
	return r.Addr.String()
}

type Resolver struct {
	trusted []netip.Prefix
}

// NewResolver builds a resolver that trusts exactly the given prefixes.
// A nil or empty list means no proxy is trusted and forwarding headers are
// always ignored.
func NewResolver(trusted []netip.Prefix) *Resolver {
	return &Resolver{trusted: trusted}
}

// Resolve determines the client address for req. The forwarded chain is only
// consulted when the direct peer is a trusted proxy; otherwise, and whenever
// the chain is malformed, oversized, or contains any invalid entry, the peer
// address wins. An unparseable peer address yields an invalid Result whose
// Key() is the shared unknown-client bucket.
func (r *Resolver) Resolve(req *http.Request) Result {
	remote, ok := parsePeerAddr(req.RemoteAddr)
	if !ok {
		return Result{Source: SourceUnknown}
	}
	remoteResult := Result{Addr: remote, Source: SourceRemote, Valid: true}

	if len(r.trusted) == 0 || !r.isTrusted(remote) {
		return remoteResult
	}

	chain, ok := parseForwardedChain(req.Header.Values("X-Forwarded-For"))
	if !ok || len(chain) == 0 {
		return remoteResult
	}

	// Full chain = forwarded entries + the direct peer; the peer is already
	// known to be trusted, so walk the forwarded part right to left and skip
	// trusted hops. The first untrusted address is the client.
	for i := len(chain) - 1; i >= 0; i-- {
		if !r.isTrusted(chain[i]) {
			return Result{Addr: chain[i], Source: SourceForwarded, Valid: true}
		}
	}
	// Every hop is a trusted proxy: the leftmost entry is the origin.
	return Result{Addr: chain[0], Source: SourceForwarded, Valid: true}
}

func (r *Resolver) isTrusted(addr netip.Addr) bool {
	for _, prefix := range r.trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// parsePeerAddr parses RemoteAddr as host:port first, then as a bare IP.
// Hostnames, empty strings, and zoned addresses are rejected; IPv4-mapped
// IPv6 is unmapped so both forms share one identity.
func parsePeerAddr(remoteAddr string) (netip.Addr, bool) {
	if remoteAddr == "" {
		return netip.Addr{}, false
	}
	if addrPort, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return normalizeAddr(addrPort.Addr())
	}
	if addr, err := netip.ParseAddr(remoteAddr); err == nil {
		return normalizeAddr(addr)
	}
	return netip.Addr{}, false
}

// parseForwardedChain merges every X-Forwarded-For value per HTTP semantics
// and parses it strictly. Any invalid entry (port, hostname, "unknown",
// empty item, zone) or exceeding the size limits invalidates the whole chain.
func parseForwardedChain(values []string) ([]netip.Addr, bool) {
	if len(values) == 0 {
		return nil, true
	}
	merged := strings.Join(values, ",")
	if strings.TrimSpace(merged) == "" {
		return nil, true
	}
	if len(merged) > maxForwardedForBytes {
		return nil, false
	}
	parts := strings.Split(merged, ",")
	if len(parts) > maxForwardedForEntries {
		return nil, false
	}
	chain := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		addr, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			return nil, false
		}
		normalized, ok := normalizeAddr(addr)
		if !ok {
			return nil, false
		}
		chain = append(chain, normalized)
	}
	return chain, true
}

func normalizeAddr(addr netip.Addr) (netip.Addr, bool) {
	if addr.Zone() != "" {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

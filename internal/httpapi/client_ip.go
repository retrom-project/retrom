package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const unknownClientIP = "unknown-peer"

func canonicalPeerIP(remoteAddress string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	address, err := netip.ParseAddr(host)
	if err != nil || address.Zone() != "" {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func trustedProxy(address netip.Addr, proxies []netip.Prefix) bool {
	for _, proxy := range proxies {
		if proxy.Contains(address) {
			return true
		}
	}
	return false
}

func canonicalClientIP(request *http.Request, trustedProxies []netip.Prefix) (string, string) {
	peer, valid := canonicalPeerIP(request.RemoteAddr)
	if !valid {
		return unknownClientIP, "CLIENT_IP_PEER_INVALID"
	}
	if !trustedProxy(peer, trustedProxies) {
		return peer.String(), ""
	}
	forwarded := request.Header.Values("X-Forwarded-For")
	if len(forwarded) != 1 || forwarded[0] == "" {
		return peer.String(), "CLIENT_IP_XFF_MISSING_OR_MULTIPLE"
	}
	parts := strings.Split(forwarded[0], ",")
	if len(parts) == 0 || len(parts) > 16 {
		return peer.String(), "CLIENT_IP_XFF_LENGTH_INVALID"
	}
	chain := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		address, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil || address.Zone() != "" {
			return peer.String(), "CLIENT_IP_XFF_ADDRESS_INVALID"
		}
		chain = append(chain, address.Unmap())
	}
	for index := len(chain) - 1; index >= 0; index-- {
		if !trustedProxy(chain[index], trustedProxies) {
			return chain[index].String(), ""
		}
	}
	return peer.String(), "CLIENT_IP_XFF_ALL_TRUSTED"
}

func (server *Server) authenticationClientIP(request *http.Request) string {
	address, diagnostic := canonicalClientIP(request, server.config.TrustedProxies)
	if diagnostic != "" {
		slog.WarnContext(request.Context(), "authentication client IP fallback", "code", diagnostic)
	}
	return address
}

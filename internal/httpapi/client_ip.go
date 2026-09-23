package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
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

func canonicalClientIP(request *http.Request) (string, string) {
	peer, valid := canonicalPeerIP(request.RemoteAddr)
	if !valid {
		return unknownClientIP, "CLIENT_IP_PEER_INVALID"
	}
	forwarded := request.Header.Values("X-Forwarded-For")
	if len(forwarded) == 0 {
		return peer.String(), ""
	}
	if len(forwarded) != 1 || forwarded[0] == "" {
		return peer.String(), "CLIENT_IP_XFF_MULTIPLE_OR_EMPTY"
	}
	address, err := netip.ParseAddr(forwarded[0])
	if err != nil || address.Zone() != "" {
		return peer.String(), "CLIENT_IP_XFF_ADDRESS_INVALID"
	}
	return address.Unmap().String(), ""
}

func (server *Server) authenticationClientIP(request *http.Request) string {
	address, diagnostic := canonicalClientIP(request)
	if diagnostic != "" {
		slog.WarnContext(request.Context(), "authentication client IP fallback", "code", diagnostic)
	}
	return address
}

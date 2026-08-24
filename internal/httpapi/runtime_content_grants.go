package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/launch"
)

const (
	runtimeContentGrantPrefix = "retrom_launch_content_"
	maxRuntimeContentGrants   = 32
	immutablePrivateContent   = "private, max-age=31536000, immutable"
)

type runtimeContentGrant struct {
	LaunchID   string
	Capability string
}

func (server *Server) setLaunchContentGrant(writer http.ResponseWriter, launchID, capability string, maxAge int) {
	http.SetCookie(writer, &http.Cookie{
		Name:     runtimeContentGrantPrefix + launchID,
		Value:    capability,
		Path:     launch.RuntimeContentPath,
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   server.config.PublicOrigin.Scheme == "https",
	})
}

func runtimeContentGrants(request *http.Request) ([]runtimeContentGrant, bool) {
	grants := make([]runtimeContentGrant, 0, 4)
	seen := make(map[string]struct{})
	for _, cookie := range request.Cookies() {
		if !strings.HasPrefix(cookie.Name, runtimeContentGrantPrefix) {
			continue
		}
		launchID := strings.TrimPrefix(cookie.Name, runtimeContentGrantPrefix)
		parsed, err := uuid.Parse(launchID)
		if err != nil || parsed.Version() != 7 || parsed.String() != launchID ||
			cookie.Value == "" || len(cookie.Value) > 128 || len(grants) >= maxRuntimeContentGrants {
			return nil, false
		}
		if _, duplicate := seen[launchID]; duplicate {
			return nil, false
		}
		seen[launchID] = struct{}{}
		grants = append(grants, runtimeContentGrant{LaunchID: launchID, Capability: cookie.Value})
	}
	return grants, len(grants) > 0
}

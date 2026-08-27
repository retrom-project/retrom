package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/launch"
)

const (
	runtimeContentGrantPrefix = "retrom_launch_content_"
	runtimeProjectPrefix      = "retrom_launch_project_"
	maxRuntimeContentGrants   = 32
	immutablePrivateContent   = "private, max-age=31536000, immutable"
)

type runtimeContentGrant struct {
	LaunchID   string
	Capability string
}

func (server *Server) setLaunchProjectGrant(
	writer http.ResponseWriter,
	launchID, capability string,
	maxAge int,
) {
	http.SetCookie(writer, &http.Cookie{
		Name:     runtimeProjectPrefix + launchID,
		Value:    capability,
		Path:     "/runtime/projects/" + launchID + "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   server.config.PublicOrigin.Scheme == "https",
	})
}

func runtimeProjectGrant(request *http.Request, launchID string) (runtimeContentGrant, bool) {
	parsed, err := uuid.Parse(launchID)
	if err != nil || parsed.Version() != 7 || parsed.String() != launchID {
		return runtimeContentGrant{}, false
	}
	name := runtimeProjectPrefix + launchID
	var capability string
	matches := 0
	for _, cookie := range request.Cookies() {
		if cookie.Name != name {
			continue
		}
		matches++
		capability = cookie.Value
	}
	if matches != 1 || capability == "" || len(capability) > 128 {
		return runtimeContentGrant{}, false
	}
	return runtimeContentGrant{LaunchID: launchID, Capability: capability}, true
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

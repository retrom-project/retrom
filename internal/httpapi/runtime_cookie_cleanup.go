package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/config"
)

// Retired cookies are only removed; they never authorize a runtime request.
func (server *Server) clearLegacyRuntimeCookies(writer http.ResponseWriter, request *http.Request) {
	for _, cookie := range request.Cookies() {
		path := legacyRuntimeCookiePath(cookie.Name)
		if path != "" {
			http.SetCookie(writer, &http.Cookie{
				Name: cookie.Name, Path: path, MaxAge: -1, HttpOnly: true,
				Secure: server.config.PublicOrigin.Scheme == "https", SameSite: http.SameSiteStrictMode,
			})
		}
	}
}

func (server *Server) clearRuntimeCookie(writer http.ResponseWriter) error {
	domain, err := config.RuntimeCookieDomain(server.config.PublicOrigin, server.config.RPGRuntimeOriginTemplate)
	if err != nil {
		return fmt.Errorf("clear runtime cookie domain: %w", err)
	}
	http.SetCookie(writer, &http.Cookie{
		Name: runtimeCookieName, Path: "/", Domain: domain, MaxAge: -1, HttpOnly: true,
		Secure: server.config.PublicOrigin.Scheme == "https", SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func legacyRuntimeCookiePath(name string) string {
	if name == "retrom_rpg_runtime" {
		return "/__retrom/"
	}
	if id, ok := strings.CutPrefix(name, "retrom_launch_content_"); ok {
		if _, err := uuid.Parse(id); err == nil {
			return "/runtime/content/"
		}
		return ""
	}
	if id, ok := strings.CutPrefix(name, "retrom_launch_"); ok {
		if parsed, err := uuid.Parse(id); err == nil && parsed.String() == id {
			return "/runtime/launches/" + id + "/"
		}
	}
	return ""
}

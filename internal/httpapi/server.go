package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/service/accounts"
	"retrom/internal/service/bios"
	"retrom/internal/service/directory"
	"retrom/internal/service/favorites"
	"retrom/internal/service/home"
	"retrom/internal/service/library"
	"retrom/internal/service/recent"
	"retrom/internal/service/runs"
	"retrom/internal/service/saves"
	"retrom/internal/service/scans"
	"retrom/internal/service/tags"
	"retrom/internal/storage"
)

type (
	Server struct {
		Accounts           *accounts.Service
		Directory          *directory.Service
		Tags               *tags.Service
		Library            *library.Service
		Favorites          *favorites.Service
		Recent             *recent.Service
		Runs               *runs.Service
		Saves              *saves.Service
		Home               *home.Service
		Bios               *bios.Service
		Scans              *scans.Service
		Runtime            *runtimeclient.Client
		Storage            *storage.Store
		Sources            storage.Sources
		WebRoot            string
		Origin             string
		CookieName         string
		TrustedProxies     []*net.IPNet
		validate           func(*http.Request) error
		decodeSaveMetadata func(string, *model.SaveInput) error
	}
	handler func(http.ResponseWriter, *http.Request, model.Principal) error
)

func (s *Server) Handler() (http.Handler, error) {
	var err error
	s.validate, err = contractValidator()
	if err != nil {
		return nil, err
	}
	s.decodeSaveMetadata, err = saveMetadataValidator()
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /health/ready", s.public(s.ready))
	mux.HandleFunc("GET /api/v1/auth/context", s.open(s.authContext))
	mux.HandleFunc("POST /api/v1/auth/login", s.open(s.login))
	mux.HandleFunc("POST /api/v1/auth/initialize", s.open(s.initialize))
	mux.HandleFunc("POST /api/v1/auth/logout", s.open(s.logout))
	mux.HandleFunc("POST /api/v1/auth/change-password", s.authorized(s.password, false))
	mux.HandleFunc("GET /api/v1/admin/users", s.authorized(s.users, true))
	mux.HandleFunc("PATCH /api/v1/admin/users/{userId}", s.authorized(s.updateUser, true))
	s.routes(mux)
	return securityHeaders(mux), nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) public(next handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r, cancel := boundedRequest(r)
		defer cancel()
		if r.Method != "GET" && r.Method != "HEAD" && !s.originValid(r) {
			writeError(w, model.ErrForbidden)
			return
		}
		if err := next(w, r, model.Principal{}); err != nil {
			writeError(w, err)
		}
	}
}

func (s *Server) open(next handler) http.HandlerFunc {
	return s.public(func(w http.ResponseWriter, r *http.Request, p model.Principal) error {
		if err := s.validate(r); err != nil {
			return err
		}
		return next(w, r, p)
	})
}

func (s *Server) authorized(next handler, admin bool) http.HandlerFunc {
	return s.public(func(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
		p, token, err := s.principal(r)
		if err != nil {
			return wrap(err)
		}
		if admin {
			if err = p.Admin(); err != nil {
				return wrap(err)
			}
		}
		validCSRF := hmac.Equal([]byte(r.Header.Get("X-Retrom-Csrf")), []byte(csrf(token)))
		if r.Method != "GET" && r.Method != "HEAD" && !validCSRF {
			return model.ErrForbidden
		}
		if err = s.validate(r); err != nil {
			return err
		}
		return next(w, r, p)
	})
}

func (s *Server) originValid(r *http.Request) bool {
	return r.Header.Get("Origin") == s.Origin && len(r.Header.Values("Origin")) == 1 &&
		(r.Header.Get("Sec-Fetch-Site") == "" || r.Header.Get("Sec-Fetch-Site") == "same-origin")
}

func (s *Server) principal(r *http.Request) (model.Principal, string, error) {
	cookie, err := r.Cookie(s.CookieName)
	if err != nil {
		return model.Principal{}, "", model.ErrUnauthorized
	}
	p, err := s.Accounts.Authenticate(r.Context(), cookie.Value)
	return p, cookie.Value, wrap(err)
}

func csrf(token string) string {
	hash := hmac.New(sha256.New, []byte(token))
	hash.Write([]byte("retrom-csrf"))
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}

func (s *Server) setCookie(w http.ResponseWriter, token string) {
	cookie := &http.Cookie{
		Name: s.CookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: strings.HasPrefix(s.Origin, "https://"), MaxAge: 86400,
	}
	if token == "" {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(0, 0)
	}
	http.SetCookie(w, cookie)
}

func decode(r *http.Request, value any) error {
	return strictJSON(http.MaxBytesReader(nil, r.Body, 1024*1024), value)
}

func respond(w http.ResponseWriter, value any) error {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

func writeError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "Request could not be completed"
	var parentError *model.ArcadeParentError
	switch {
	case errors.As(err, &parentError):
		status, code, message = 400, parentError.Code, parentError.Message
	case errors.Is(err, model.ErrInvalid):
		status, code, message = 400, "INVALID_INPUT", "Input is invalid"
	case errors.Is(err, model.ErrUnauthorized):
		status, code, message = 401, "AUTHENTICATION_REQUIRED", "Sign in to continue"
	case errors.Is(err, model.ErrForbidden):
		status, code, message = 403, "FORBIDDEN", "Operation is not permitted"
	case errors.Is(err, model.ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "Item is unavailable"
	case errors.Is(err, model.ErrTagNameConflict):
		status, code, message = 409, "TAG_NAME_CONFLICT", "A tag with this name already exists"
	case errors.Is(err, model.ErrConflict):
		status, code, message = 409, "VERSION_CONFLICT", "Item changed; refresh and try again"
	case errors.Is(err, model.ErrUnavailable):
		status, code, message = 503, "SERVICE_UNAVAILABLE", "Service is temporarily unavailable"
	case errors.Is(err, model.ErrRateLimited):
		status, code, message = 429, "RATE_LIMITED", "Try again later"
		w.Header().Set("Retry-After", "900")
	case errors.Is(err, model.ErrBIOSMissing):
		status, code, message = 409, "BIOS_MISSING", "Required BIOS is not installed; check BIOS management"
	case errors.Is(err, model.ErrContextExpired):
		status, code, message = 409, "CONTEXT_EXPIRED", "Run context expired; preserve unsaved progress"
	}
	if status == 500 {
		slog.Error("HTTP request failed", "error", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if encodeErr := json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message}); encodeErr != nil {
		slog.Error("write HTTP error", "error", encodeErr)
	}
}

func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "unknown"
	}
	remote := net.ParseIP(host)
	if !s.trusted(remote) {
		return host
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) != 1 {
		return host
	}
	chain := strings.Split(values[0], ",")
	if len(chain) > 16 {
		return host
	}
	for i := len(chain) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(chain[i]))
		if ip == nil {
			return host
		}
		if !s.trusted(ip) {
			return ip.String()
		}
	}
	return host
}

func (s *Server) trusted(ip net.IP) bool {
	for _, network := range s.TrustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("HTTP operation: %w", err)
}

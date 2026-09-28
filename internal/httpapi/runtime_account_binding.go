package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/authn"
	"retrom/internal/service/runtimesession"
)

func (server *Server) runtimeSessionForPrincipal(
	request *http.Request, principal authn.Principal,
) (runtimesession.Session, error) {
	current, err := server.readRuntimeSession(request)
	if err == nil && current.ProfileID == principal.ProfileID {
		return current, nil
	}
	if err != nil && !errors.Is(err, runtimesession.ErrCredential) {
		return runtimesession.Session{}, err
	}
	issued, err := server.playDeps.RuntimeSessions.Ensure(request.Context(), principal.SessionID)
	if err != nil {
		return issued, fmt.Errorf("issue runtime session: %w", err)
	}
	return issued, nil
}

func (server *Server) clearRuntimeAfterAccountSwitch(
	writer http.ResponseWriter, request *http.Request, principal authn.Principal,
) bool {
	current, err := server.readRuntimeSession(request)
	if errors.Is(err, runtimesession.ErrCredential) {
		return true
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return false
	}
	if current.ProfileID == principal.ProfileID {
		if current.Refreshed {
			if err := server.setRuntimeCookie(writer, current); err != nil {
				server.databaseError(writer, request, err)
				return false
			}
		}
		return true
	}
	if err := server.accountDeps.Accounts.Logout(request.Context(), current.AuthSessionID); err != nil {
		server.databaseError(writer, request, err)
		return false
	}
	if err := server.clearRuntimeCookie(writer); err != nil {
		server.databaseError(writer, request, err)
		return false
	}
	return true
}

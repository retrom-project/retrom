package httpapi

import (
	"errors"
	"net/http"

	"retrom/internal/adapter/runtime/launch"
)

// writeGeneratedProjectIndex returns false only for the frozen static-index path.
func (server *Server) writeGeneratedProjectIndex(
	writer http.ResponseWriter, request *http.Request, index launch.ProjectIndexView, err error,
) bool {
	if errors.Is(err, launch.ErrProjectIndexUnavailable) {
		return false
	}
	if errors.Is(err, launch.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "项目内容不可用", map[string]any{})
		return true
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return true
	}
	serveProjectIndex(writer, request, index)
	return true
}

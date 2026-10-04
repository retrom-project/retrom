package httpapi

import (
	"net/http"

	"retrom/internal/httpapi/generated"
)

func (server *Server) webConfig(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, generated.WebConfig{
		RuntimeOriginTemplate: server.config.RPGRuntimeOriginTemplate,
	})
}

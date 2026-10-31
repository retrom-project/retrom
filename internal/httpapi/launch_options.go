package httpapi

import (
	"encoding/json"
	"net/http"

	"retrom/internal/model"
)

func (s *Server) launchOptions(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var config json.RawMessage
	if err := decode(r, &config); err != nil {
		return err
	}
	options, err := s.Library.LaunchOptions(r.Context(), p, r.PathValue("gameId"), config)
	if err != nil {
		return wrap(err)
	}
	return respond(w, options)
}

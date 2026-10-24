package httpapi

import (
	"net/http"

	"retrom/internal/model"
)

func (s *Server) contentReplacement(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	value, err := s.Scans.ContentReplacement(r.Context(), p, r.PathValue("gameId"), r.PathValue("operationId"))
	if err != nil {
		return wrap(err)
	}
	return respond(w, value)
}

func (s *Server) cancelContentReplacement(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	value, err := s.Scans.CancelReplacement(r.Context(), p, r.PathValue("gameId"), r.PathValue("operationId"))
	if err != nil {
		return wrap(err)
	}
	return respond(w, value)
}

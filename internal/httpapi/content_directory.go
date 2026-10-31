package httpapi

import (
	"net/http"
	"strconv"

	"retrom/internal/model"
)

func (s *Server) contentPage(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	limit, err := contentPageLimit(r)
	if err != nil {
		return err
	}
	page, err := s.Library.ContentPage(r.Context(), p, r.PathValue("gameId"),
		r.URL.Query().Get("after"), r.URL.Query().Get("prefix"), limit)
	if err != nil {
		return wrap(err)
	}
	return respond(w, page)
}

func (s *Server) kirikiriEntryCandidates(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	limit, err := contentPageLimit(r)
	if err != nil {
		return err
	}
	page, err := s.Library.KirikiriEntryCandidates(r.Context(), p, r.PathValue("gameId"),
		r.URL.Query().Get("after"), r.URL.Query().Get("prefix"), limit)
	if err != nil {
		return wrap(err)
	}
	return respond(w, page)
}

func contentPageLimit(r *http.Request) (int, error) {
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return 0, model.ErrInvalid
		}
		limit = parsed
	}
	return limit, nil
}

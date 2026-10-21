package httpapi

import (
	"net/http"
	"strconv"

	"retrom/internal/model"
)

func (s *Server) contentPage(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return model.ErrInvalid
		}
		limit = parsed
	}
	page, err := s.Library.ContentPage(r.Context(), p, r.PathValue("gameId"),
		r.URL.Query().Get("after"), r.URL.Query().Get("prefix"), limit)
	if err != nil {
		return wrap(err)
	}
	return respond(w, page)
}

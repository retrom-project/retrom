package httpapi

import (
	"net/http"
	"strings"

	"retrom/internal/model"
)

func gameStatus(r *http.Request) string {
	if strings.Contains(r.URL.Path, "/reviews") {
		return "pending_review"
	}
	return "published"
}

func (s *Server) games(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	q, err := query(r)
	if err != nil {
		return err
	}
	items, err := s.Library.List(r.Context(), p, gameStatus(r), q)
	if err != nil {
		return wrap(err)
	}
	return respond(w, items)
}

func (s *Server) game(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	result, err := s.Library.Detail(r.Context(), p, r.PathValue("gameId"), gameStatus(r))
	if err != nil {
		return wrap(err)
	}
	saves, err := s.Saves.List(r.Context(), p, result.Game.ID, model.Query{Limit: 100})
	if err != nil {
		return wrap(err)
	}
	result.Saves = saves.Items
	folders, err := s.Favorites.Membership(r.Context(), p, result.Game.ID)
	if err != nil {
		return wrap(err)
	}
	result.FavoriteFolderIDs = folders
	return respond(w, result)
}

func (s *Server) writeGame(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input model.GameInput
	if err := decode(r, &input); err != nil {
		return err
	}
	item, err := s.Library.Update(r.Context(), p, r.PathValue("gameId"), gameStatus(r), input)
	if err != nil {
		return wrap(err)
	}
	return respond(w, item)
}

func (s *Server) deleteGame(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	version, err := versionBody(r)
	if err != nil {
		return err
	}
	return noContent(w, s.Library.Delete(r.Context(), p, r.PathValue("gameId"), gameStatus(r), version))
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	version, err := versionBody(r)
	if err != nil {
		return err
	}
	item, err := s.Library.Approve(r.Context(), p, r.PathValue("gameId"), version)
	if err != nil {
		return wrap(err)
	}
	return respond(w, item)
}

func (s *Server) reviewReadiness(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input model.ReviewReadinessRequest
	if err := decode(r, &input); err != nil {
		return err
	}
	items, err := s.Library.Readiness(r.Context(), p, input.GameIDs)
	if err != nil {
		return wrap(err)
	}
	return respond(w, items)
}

func (s *Server) media(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	key, err := s.Library.Media(r.Context(), p, r.PathValue("gameId"), r.PathValue("mediaId"))
	if err != nil {
		return wrap(err)
	}
	return s.serveManaged(w, r, key)
}

func (s *Server) home(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	result, err := s.Home.Get(r.Context(), p)
	if err != nil {
		return wrap(err)
	}
	return respond(w, result)
}

func (s *Server) recent(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	q, err := query(r)
	if err != nil {
		return err
	}
	items, err := s.Recent.List(r.Context(), p, q)
	if err != nil {
		return wrap(err)
	}
	return respond(w, items)
}

func (s *Server) scummvmCandidates(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	result, err := s.Library.ScummvmCandidates(r.Context(), p, r.PathValue("gameId"))
	if err != nil {
		return wrap(err)
	}
	return respond(w, result)
}

func (s *Server) dosEntryCandidates(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	result, err := s.Library.DOSEntryCandidates(r.Context(), p, r.PathValue("gameId"), r.URL.Query().Get("entryFile"))
	if err != nil {
		return wrap(err)
	}
	return respond(w, result)
}

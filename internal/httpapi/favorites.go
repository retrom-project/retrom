package httpapi

import (
	"net/http"

	"retrom/internal/model"
)

func (s *Server) favorites(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	q, err := query(r)
	if err != nil {
		return err
	}
	items, err := s.Favorites.List(r.Context(), p, q)
	if err != nil {
		return wrap(err)
	}
	return respond(w, items)
}

func (s *Server) setFavorite(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input struct {
		FolderIDs []string `json:"folderIds"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	return noContent(w, s.Favorites.Set(r.Context(), p, r.PathValue("gameId"), input.FolderIDs))
}

func (s *Server) removeFavorite(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	return noContent(w, s.Favorites.Remove(r.Context(), p, r.PathValue("gameId")))
}

func (s *Server) folders(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	items, err := s.Favorites.Folders(r.Context(), p)
	if err != nil {
		return wrap(err)
	}
	return respond(w, model.List[model.Folder]{Items: items})
}

func (s *Server) writeFolder(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input struct {
		Name    string `json:"name"`
		Version int64  `json:"version"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	item, err := s.Favorites.WriteFolder(r.Context(), p, r.PathValue("folderId"), input.Name, input.Version)
	if err != nil {
		return wrap(err)
	}
	return respond(w, item)
}

func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	return noContent(w, s.Favorites.DeleteFolder(r.Context(), p, r.PathValue("folderId")))
}

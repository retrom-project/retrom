package httpapi

import (
	"net/http"
	"strings"

	"retrom/internal/model"
)

func (s *Server) catalog(w http.ResponseWriter, _ *http.Request, _ model.Principal) error {
	return respond(w, s.Runtime.Catalog)
}

func (s *Server) directories(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	items, err := s.Directory.List(r.Context(), strings.Contains(r.URL.Path, "/admin/"))
	if err != nil {
		return wrap(err)
	}
	return respond(w, model.List[model.Directory]{Items: items})
}

func (s *Server) writeDirectory(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input model.DirectoryInput
	if err := decode(r, &input); err != nil {
		return err
	}
	item, err := s.Directory.Write(r.Context(), p, r.PathValue("directoryId"), input)
	if err != nil {
		return wrap(err)
	}
	return respond(w, item)
}

func (s *Server) deleteDirectory(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	version, err := versionBody(r)
	if err != nil {
		return err
	}
	return noContent(w, s.Directory.Delete(r.Context(), p, r.PathValue("directoryId"), version))
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	q, err := query(r)
	if err != nil {
		return err
	}
	items, err := s.Tags.List(r.Context(), q)
	if err != nil {
		return wrap(err)
	}
	return respond(w, items)
}

func (s *Server) writeTag(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input struct {
		Name    string `json:"name"`
		Version int64  `json:"version"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	item, err := s.Tags.Write(r.Context(), p, r.PathValue("tagId"), input.Name, input.Version)
	if err != nil {
		return wrap(err)
	}
	return respond(w, item)
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	version, err := versionBody(r)
	if err != nil {
		return err
	}
	return noContent(w, s.Tags.Delete(r.Context(), p, r.PathValue("tagId"), version))
}

func versionBody(r *http.Request) (int64, error) {
	var input struct {
		Version int64 `json:"version"`
	}
	if err := decode(r, &input); err != nil {
		return 0, err
	}
	if input.Version < 1 {
		return 0, model.ErrInvalid
	}
	return input.Version, nil
}

func noContent(w http.ResponseWriter, err error) error {
	if err != nil {
		return wrap(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

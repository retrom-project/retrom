package httpapi

import (
	"net/http"
	"strconv"

	"retrom/internal/model"
)

func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	r.Body = http.MaxBytesReader(w, r.Body, 513*1024*1024)
	if err := r.ParseMultipartForm(1024 * 1024); err != nil {
		return model.ErrInvalid
	}
	defer removeMultipart(r.MultipartForm)
	version, err := strconv.ParseInt(r.FormValue("version"), 10, 64)
	if err != nil {
		return model.ErrInvalid
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return model.ErrInvalid
	}
	defer closeFile(file)
	result, err := s.Library.UploadMedia(r.Context(), p, r.PathValue("gameId"), r.FormValue("kind"), version, file)
	if err != nil {
		return wrap(err)
	}
	return respond(w, result)
}

func (s *Server) deleteMedia(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	version, err := versionBody(r)
	if err != nil {
		return err
	}
	return noContent(w, s.Library.RemoveMedia(r.Context(), p, r.PathValue("gameId"), r.PathValue("mediaId"), version))
}

func (s *Server) replaceContent(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input struct {
		Path    string `json:"path"`
		Version int64  `json:"version"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	value, err := s.Scans.Replace(r.Context(), p, r.PathValue("gameId"), input.Path, input.Version)
	if err != nil {
		return wrap(err)
	}
	return respond(w, value)
}

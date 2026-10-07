package httpapi

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"retrom/internal/model"
)

func (s *Server) saves(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	q, err := query(r)
	if err != nil {
		return err
	}
	page, err := s.Saves.List(r.Context(), p, r.URL.Query().Get("gameId"), q)
	if err != nil {
		return wrap(err)
	}
	return respond(w, page)
}

func (s *Server) writeSave(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	r.Body = http.MaxBytesReader(w, r.Body, 280*1024*1024)
	if err := r.ParseMultipartForm(1024 * 1024); err != nil {
		return fmt.Errorf("multipart save: %w", model.ErrInvalid)
	}
	defer removeMultipart(r.MultipartForm)
	var input model.SaveInput
	if len(r.MultipartForm.Value) != 1 || len(r.MultipartForm.Value["metadata"]) != 1 {
		return model.ErrInvalid
	}
	for name, parts := range r.MultipartForm.File {
		if !model.OneOf(name, "payload", "screenshot") || len(parts) != 1 {
			return model.ErrInvalid
		}
	}
	if err := s.decodeSaveMetadata(r.FormValue("metadata"), &input); err != nil {
		return err
	}
	payload, _, err := r.FormFile("payload")
	if err != nil {
		return model.ErrInvalid
	}
	defer closeFile(payload)
	var screenshot io.Reader
	image, _, imageErr := r.FormFile("screenshot")
	if imageErr == nil {
		defer closeFile(image)
		screenshot = image
	} else if !errors.Is(imageErr, http.ErrMissingFile) {
		return fmt.Errorf("save screenshot: %w", imageErr)
	}
	save, err := s.Saves.Write(r.Context(), p, r.PathValue("saveId"), input, payload, screenshot)
	if err != nil {
		return wrap(err)
	}
	return respond(w, save)
}

func (s *Server) renameSave(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input struct {
		Name    string `json:"name"`
		Version int64  `json:"version"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	value, err := s.Saves.Rename(r.Context(), p, r.PathValue("saveId"), input.Name, input.Version)
	if err != nil {
		return wrap(err)
	}
	return respond(w, value)
}

func (s *Server) deleteSave(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	version, err := versionBody(r)
	if err != nil {
		return err
	}
	return noContent(w, s.Saves.Delete(r.Context(), p, r.PathValue("saveId"), version))
}

func (s *Server) saveFile(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	image := strings.HasSuffix(r.URL.Path, "/screenshot")
	file, err := s.Saves.File(r.Context(), p, r.PathValue("saveId"), image)
	if err != nil {
		return wrap(err)
	}
	if file.SHA256 != "" {
		w.Header().Set("ETag", "\"sha256-"+file.SHA256+"\"")
	}
	return s.serveManaged(w, r, file.Key)
}

func removeMultipart(form *multipart.Form) {
	if form != nil {
		if err := form.RemoveAll(); err != nil {
			logMultipart(err)
		}
	}
}

package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"retrom/internal/model"
)

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input model.UploadInput
	if err := strictJSON(http.MaxBytesReader(nil, r.Body, 16*1024*1024), &input); err != nil {
		return err
	}
	upload, err := s.Scans.CreateUpload(r.Context(), p, input)
	if err != nil {
		return wrap(err)
	}
	return respond(w, upload)
}

func (s *Server) uploads(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	items, err := s.Scans.ListUploads(r.Context(), p)
	if err != nil {
		return wrap(err)
	}
	return respond(w, model.List[model.Upload]{Items: items})
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	upload, err := s.Scans.Upload(r.Context(), p, r.PathValue("uploadId"))
	if err != nil {
		return wrap(err)
	}
	return respond(w, upload)
}

func (s *Server) writeUpload(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		return model.ErrInvalid
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || offset < 0 {
		return model.ErrInvalid
	}
	r.Body = http.MaxBytesReader(w, r.Body, model.UploadChunkBytes)
	progress, err := s.Scans.WriteUpload(r.Context(), p, r.PathValue("uploadId"), index, offset, r.Body)
	if tooLarge := new(http.MaxBytesError); errors.As(err, &tooLarge) {
		return model.ErrInvalid
	}
	if err != nil {
		return wrap(err)
	}
	return respond(w, progress)
}

func (s *Server) deleteUpload(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	return noContent(w, s.Scans.DeleteUpload(r.Context(), p, r.PathValue("uploadId")))
}

package httpapi

import (
	"net/http"

	"retrom/internal/model"
	"retrom/internal/storage"
)

func (s *Server) arcadeParentOptions(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	result, err := s.Library.ArcadeParentOptions(r.Context(), p, r.PathValue("gameId"), r.URL.Query().Get("coreId"))
	if err != nil {
		return wrap(err)
	}
	return respond(w, result)
}

func (s *Server) uploadParent(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	r.Body = http.MaxBytesReader(w, r.Body, storage.MaximumFileSize+1024*1024)
	if err := r.ParseMultipartForm(1024 * 1024); err != nil {
		return model.ErrInvalid
	}
	defer removeMultipart(r.MultipartForm)
	version, core, err := parentUploadMetadata(r.MultipartForm)
	if err != nil {
		return model.ErrInvalid
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return model.ErrInvalid
	}
	defer closeFile(file)
	result, err := s.Library.UploadParent(r.Context(), p, r.PathValue("gameId"), core,
		header.Filename, version, file)
	if err != nil {
		return wrap(err)
	}
	return respond(w, result)
}

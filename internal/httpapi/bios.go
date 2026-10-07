package httpapi

import (
	"log/slog"
	"net/http"

	"retrom/internal/model"
)

func (s *Server) bios(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	items, err := s.Bios.List(r.Context())
	if err != nil {
		return wrap(err)
	}
	return respond(w, model.List[model.BiosRequirement]{Items: items})
}

func (s *Server) uploadBios(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	r.Body = http.MaxBytesReader(w, r.Body, 513*1024*1024)
	if err := r.ParseMultipartForm(1024 * 1024); err != nil {
		return model.ErrInvalid
	}
	defer removeMultipart(r.MultipartForm)
	file, header, err := r.FormFile("file")
	if err != nil {
		return model.ErrInvalid
	}
	defer closeFile(file)
	value, err := s.Bios.Upload(r.Context(), p, r.PathValue("requirementKey"), header.Filename, file)
	if err != nil {
		return wrap(err)
	}
	return respond(w, value)
}

func (s *Server) deleteBios(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	return noContent(w, s.Bios.Delete(r.Context(), p, r.PathValue("requirementKey")))
}

func (s *Server) sourceRoots(w http.ResponseWriter, _ *http.Request, _ model.Principal) error {
	return respond(w, model.List[model.Root]{Items: s.Sources.Roots})
}

func (s *Server) sourceDirectories(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	items, err := s.Sources.Directories(r.PathValue("rootId"), r.URL.Query().Get("relativePath"))
	if err != nil {
		return wrap(err)
	}
	return respond(w, model.List[model.SourceDirectory]{Items: items})
}
func logMultipart(err error) { slog.Error("remove multipart temporary files", "error", err) }

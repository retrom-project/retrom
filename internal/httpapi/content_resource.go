package httpapi

import (
	"errors"
	"net/http"
	"os"
	"time"

	"retrom/internal/model"
	"retrom/internal/service/runs"
)

func (s *Server) serveRunBlob(w http.ResponseWriter, r *http.Request, blob runs.Blob) error {
	w.Header().Set("Content-Type", blob.MediaType)
	w.Header().Set("ETag", "\"sha256-"+blob.SHA256+"\"")
	file, err := s.Storage.OpenContent(blob.Key, blob.Offset, blob.SizeBytes)
	if errors.Is(err, os.ErrNotExist) {
		return model.ErrNotFound
	}
	if err != nil {
		return wrap(err)
	}
	defer closeFile(file)
	w.Header().Set("Cache-Control", "private, no-store, no-transform")
	http.ServeContent(w, r, "", time.Time{}, file.ServeContentReader())
	return nil
}

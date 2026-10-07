package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"retrom/internal/model"
	"retrom/internal/service/runs"
	"retrom/internal/storage"
)

func (s *Server) createRun(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input model.RunInput
	if err := decode(r, &input); err != nil {
		return err
	}
	run, err := s.Runs.Create(r.Context(), p, input)
	if err != nil {
		return wrap(err)
	}
	return respond(w, run)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	run, err := s.Runs.Get(r.Context(), p, r.PathValue("runId"))
	if err != nil {
		return wrap(err)
	}
	return respond(w, run.Run)
}

func (s *Server) closeRun(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	return noContent(w, s.Runs.Close(r.Context(), p, r.PathValue("runId")))
}

func (s *Server) runEvent(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input struct {
		Event string `json:"event"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	return noContent(w, s.Runs.Event(r.Context(), p, r.PathValue("runId"), input.Event))
}

func (s *Server) runResource(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	blob, index, err := s.Runs.Resource(r.Context(), p, r.PathValue("runId"), r.PathValue("resourceId"))
	if err != nil {
		return wrap(err)
	}
	if index != nil || r.PathValue("filename") != blob.Filename {
		return model.ErrNotFound
	}
	w.Header().Set("Content-Type", blob.MediaType)
	w.Header().Set("ETag", "\"sha256-"+blob.SHA256+"\"")
	return s.serveManaged(w, r, blob.Key)
}

func (s *Server) runIndex(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	_, index, err := s.Runs.Resource(r.Context(), p, r.PathValue("runId"), r.PathValue("indexId"))
	if err != nil {
		return wrap(err)
	}
	if index == nil {
		return model.ErrNotFound
	}
	files := make([]map[string]any, 0, len(index))
	for _, file := range index {
		files = append(files, map[string]any{
			"path": file.LogicalPath, "url": runs.ResourceURL(r.PathValue("runId"), file),
			"sha256": file.SHA256, "sizeBytes": file.SizeBytes, "mediaType": file.MediaType,
		})
	}
	return respond(w, map[string]any{"schemaVersion": 1, "files": files})
}

func (s *Server) serveManaged(w http.ResponseWriter, r *http.Request, key string) error {
	file, err := s.Storage.Read(key)
	if errors.Is(err, os.ErrNotExist) {
		return model.ErrNotFound
	}
	if err != nil {
		return wrap(err)
	}
	defer closeFile(file)
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat resource: %w", err)
	}
	if !info.Mode().IsRegular() {
		return model.ErrNotFound
	}
	http.ServeContent(w, r, "", info.ModTime(), file)
	return nil
}

func (s *Server) providerAsset(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	provider, exists := s.Runtime.Providers[r.PathValue("providerId")]
	if !exists || provider.BundleSHA256 != r.PathValue("bundle") {
		return model.ErrNotFound
	}
	name := r.PathValue("asset")
	if !storage.SafeRelative(name) {
		return model.ErrInvalid
	}
	if data, exists := s.Runtime.Overrides[provider.ProviderID][name]; exists {
		http.ServeContent(w, r, name, time.Unix(0, 0), bytes.NewReader(data))
		return nil
	}
	root, err := os.OpenRoot(provider.Root)
	if err != nil {
		return fmt.Errorf("open Provider resource: %w", err)
	}
	defer closeRoot(root)
	file, err := root.Open(name)
	if err != nil {
		return model.ErrNotFound
	}
	defer closeFile(file)
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat Provider resource: %w", err)
	}
	if !info.Mode().IsRegular() {
		return model.ErrNotFound
	}
	if strings.HasPrefix(filepath.Base(name), ".") || !s.Runtime.AssetAllowed(provider.ProviderID, name) {
		return model.ErrNotFound
	}
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	http.ServeContent(w, r, name, info.ModTime(), file)
	return nil
}

func (s *Server) isolationAsset(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	name := r.PathValue("asset")
	if name != "shell.html" && name != "service-worker.js" {
		return model.ErrNotFound
	}
	if !model.UUID(r.PathValue("runId")) {
		return model.ErrInvalid
	}
	w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'unsafe-inline' 'self'; worker-src 'self'; "+
			"connect-src 'self'; frame-ancestors "+s.Origin)
	if name == "service-worker.js" {
		w.Header().Set("Service-Worker-Allowed", "/run/"+r.PathValue("runId")+"/")
	}
	http.ServeFile(w, r, filepath.Join(s.WebRoot, "public/runtime-isolation", name))
	return nil
}

func closeFile(file io.Closer) {
	if err := file.Close(); err != nil {
		slog.Error("close HTTP resource", "error", err)
	}
}

func closeRoot(root *os.Root) {
	if err := root.Close(); err != nil {
		slog.Error("close resource root", "error", err)
	}
}

// Package runtimeprovider owns the active Provider projection and its static boundary.
package runtimeprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/runtimebundle"
)

var ErrInstallationInvalid = errors.New("RUNTIME_PROVIDER_INSTALLATION_INVALID")

var (
	providerIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	digestPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	publicMediaTypes  = map[string]bool{
		"text/javascript; charset=utf-8":  true,
		"text/css; charset=utf-8":         true,
		"text/plain; charset=utf-8":       true,
		"application/json; charset=utf-8": true,
		"application/wasm":                true,
		"application/octet-stream":        true,
		"application/zip":                 true,
		"application/x-7z-compressed":     true,
		"image/png":                       true,
		"image/jpeg":                      true,
		"image/gif":                       true,
		"image/webp":                      true,
		"image/svg+xml":                   true,
		"image/x-icon":                    true,
		"audio/ogg":                       true,
		"audio/mpeg":                      true,
		"audio/wav":                       true,
		"font/woff":                       true,
		"font/woff2":                      true,
	}
	rangeMediaTypes = map[string]bool{
		"application/wasm":            true,
		"application/octet-stream":    true,
		"application/zip":             true,
		"application/x-7z-compressed": true,
		"audio/ogg":                   true,
		"audio/mpeg":                  true,
		"audio/wav":                   true,
	}
	nonPublicPaths = map[string]bool{
		".installation.json":    true,
		"integrity.json":        true,
		"provider.json":         true,
		"provenance.json":       true,
		"provider-build.json":   true,
		"provider-release.json": true,
	}
)

type staticHandler struct {
	files map[string]staticFile
}

type staticFile struct {
	path      string
	sizeBytes int64
	sha256    string
	mediaType string
}

func NewStaticHandler(
	installedRoot string,
	active runtimebundle.ActiveDescriptor,
	integrityByProvider map[string][]runtimebundle.IntegrityFile,
) (http.Handler, error) {
	root, err := filepath.Abs(installedRoot)
	if err != nil {
		return nil, installationInvalid(err)
	}
	result := &staticHandler{files: make(map[string]staticFile)}
	providerIDs := make(map[string]bool, len(active.Providers))
	for _, provider := range active.Providers {
		if providerIDs[provider.ProviderID] {
			return nil, ErrInstallationInvalid
		}
		providerIDs[provider.ProviderID] = true
		files, exists := integrityByProvider[provider.ProviderID]
		if !exists {
			return nil, ErrInstallationInvalid
		}
		staticFiles, err := addStaticProvider(root, provider, files)
		if err != nil {
			return nil, err
		}
		for path, file := range staticFiles {
			key := provider.ProviderID + "\x00" + provider.BundleSHA256 + "\x00" + path
			result.files[key] = file
		}
	}
	if len(providerIDs) != len(integrityByProvider) {
		return nil, ErrInstallationInvalid
	}
	return result, nil
}

func addStaticProvider(
	root string,
	provider runtimebundle.ActiveProvider,
	files []runtimebundle.IntegrityFile,
) (map[string]staticFile, error) {
	if !providerIDPattern.MatchString(provider.ProviderID) ||
		!digestPattern.MatchString(provider.BundleSHA256) ||
		provider.InstallationPath != provider.ProviderID+"/"+provider.BundleSHA256 || len(files) == 0 {
		return nil, ErrInstallationInvalid
	}
	result := make(map[string]staticFile)
	seen := make(map[string]bool, len(files))
	clientModuleVerified := false
	for _, file := range files {
		if seen[file.Path] {
			return nil, ErrInstallationInvalid
		}
		seen[file.Path] = true
		static, isClientModule, err := loadStaticFile(root, provider, file)
		if err != nil {
			return nil, err
		}
		clientModuleVerified = clientModuleVerified || isClientModule
		if static != nil {
			result[file.Path] = *static
		}
	}
	if !clientModuleVerified {
		return nil, ErrInstallationInvalid
	}
	return result, nil
}

func loadStaticFile(
	root string,
	provider runtimebundle.ActiveProvider,
	file runtimebundle.IntegrityFile,
) (*staticFile, bool, error) {
	if !safeBundlePath(file.Path) || file.SizeBytes < 0 ||
		!digestPattern.MatchString(file.SHA256) || !publicMediaTypes[file.MediaType] {
		return nil, false, ErrInstallationInvalid
	}
	fullPath := filepath.Join(
		root,
		filepath.FromSlash(provider.InstallationPath),
		filepath.FromSlash(file.Path),
	)
	if err := verifyInstalledFile(fullPath, file); err != nil {
		return nil, false, err
	}
	isClientModule := file.Path == provider.ClientModulePath
	if isClientModule && (file.Path != "client.mjs" ||
		file.MediaType != "text/javascript; charset=utf-8" || file.SHA256 != provider.ModuleSHA256) {
		return nil, false, ErrInstallationInvalid
	}
	if nonPublicPaths[file.Path] {
		return nil, isClientModule, nil
	}
	return &staticFile{
		path:      fullPath,
		sizeBytes: file.SizeBytes,
		sha256:    file.SHA256,
		mediaType: file.MediaType,
	}, isClientModule, nil
}

func (handler *staticHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	providerID, bundleDigest, path, ok := splitProviderPath(request.URL.Path)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	file, exists := handler.files[providerID+"\x00"+bundleDigest+"\x00"+path]
	if !exists {
		http.NotFound(writer, request)
		return
	}
	rangeHeader := request.Header.Get("Range")
	if rangeHeader != "" && (!rangeMediaTypes[file.mediaType] || strings.Contains(rangeHeader, ",")) {
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", file.sizeBytes))
		writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	body, err := os.Open(file.path)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer func() { cleanup.Error("close provider body", body.Close()) }()
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writer.Header().Set("Content-Type", file.mediaType)
	writer.Header().Set("ETag", `"`+file.sha256+`"`)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	if rangeMediaTypes[file.mediaType] {
		writer.Header().Set("Accept-Ranges", "bytes")
	}
	http.ServeContent(writer, request, filepath.Base(file.path), time.Unix(0, 0), body)
}

func splitProviderPath(path string) (string, string, string, bool) {
	const prefix = "/runtime/providers/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(path, prefix), "/", 3)
	if len(parts) != 3 || !providerIDPattern.MatchString(parts[0]) || !digestPattern.MatchString(parts[1]) ||
		!safeBundlePath(parts[2]) || nonPublicPaths[parts[2]] {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func safeBundlePath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\?#\x00") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func verifyInstalledFile(path string, expected runtimebundle.IntegrityFile) error {
	metadata, err := os.Lstat(path)
	if err != nil || !metadata.Mode().IsRegular() || metadata.Size() != expected.SizeBytes {
		return ErrInstallationInvalid
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(path) {
		return ErrInstallationInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return installationInvalid(err)
	}
	defer func() { cleanup.Error("close provider file", file.Close()) }()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return installationInvalid(err)
	}
	if hex.EncodeToString(digest.Sum(nil)) != expected.SHA256 {
		return ErrInstallationInvalid
	}
	return nil
}

func installationInvalid(err error) error {
	return fmt.Errorf("%w: %w", ErrInstallationInvalid, err)
}

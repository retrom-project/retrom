package runtimeprovider

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"retrom/internal/cleanup"

	"github.com/andybalholm/brotli"
)

// Only verified siblings in the same provider generation may encode an asset.
// Range requests always address the original, uncompressed representation.
func (handler *staticHandler) compressedFile(
	writer http.ResponseWriter, request *http.Request, key string, file staticFile, development bool,
) staticFile {
	if file.mediaType != "application/wasm" && file.mediaType != "text/javascript; charset=utf-8" {
		return file
	}
	files := handler.files
	if development {
		files = handler.devFiles
	}
	encoded, exists := files[key+".br"]
	if !exists {
		return file
	}
	writer.Header().Add("Vary", "Accept-Encoding")
	if request.Header.Get("Range") != "" || !acceptsBrotli(request.Header.Get("Accept-Encoding")) {
		return file
	}
	encoded.mediaType = file.mediaType
	writer.Header().Set("Content-Encoding", "br")
	writer.Header().Set("Content-Length", strconv.FormatInt(encoded.sizeBytes, 10))
	return encoded
}

func acceptsBrotli(header string) bool {
	return acceptsEncoding(header, "br")
}

func acceptsEncoding(header, encoding string) bool {
	wildcard := false
	for _, entry := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(entry), ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		if name != encoding && name != "*" {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if !ok || !strings.EqualFold(name, "q") {
				quality = 0
				break
			}
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || parsed < 0 || parsed > 1 {
				quality = 0
				break
			}
			quality = parsed
		}
		if name == encoding {
			return quality > 0
		}
		wildcard = quality > 0
	}
	return wildcard
}

// MAME keeps one verified source representation in the Provider. Compression is
// streamed only for whole-file requests; Range always uses the original bytes.
func (handler *staticHandler) serveMameCompressed(
	writer http.ResponseWriter, request *http.Request, providerID, path string, file staticFile, development bool,
) bool {
	if providerID != "retrom-runtime" || !strings.HasPrefix(path, "assets/mame/") ||
		(file.mediaType != "application/wasm" && file.mediaType != "text/javascript; charset=utf-8") ||
		writer.Header().Get("Content-Encoding") != "" {
		return false
	}
	writer.Header().Set("Vary", "Accept-Encoding")
	if request.Header.Get("Range") != "" {
		return false
	}
	encoding := ""
	if acceptsEncoding(request.Header.Get("Accept-Encoding"), "br") {
		encoding = "br"
	} else if acceptsEncoding(request.Header.Get("Accept-Encoding"), "gzip") {
		encoding = "gzip"
	}
	if encoding == "" {
		return false
	}
	var source io.Reader
	if development {
		source = bytes.NewReader(file.contents)
	} else {
		body, err := os.Open(file.path)
		if err != nil {
			http.NotFound(writer, request)
			return true
		}
		defer func() { cleanup.Error("close MAME provider body", body.Close()) }()
		source = body
	}
	writer.Header().Set("Content-Encoding", encoding)
	writer.Header().Del("Accept-Ranges")
	etag := fmt.Sprintf(`W/"%s-%s"`, file.sha256, encoding)
	writer.Header().Set("ETag", etag)
	if request.Header.Get("If-None-Match") == etag {
		writer.WriteHeader(http.StatusNotModified)
		return true
	}
	if request.Method == http.MethodHead {
		writer.WriteHeader(http.StatusOK)
		return true
	}
	if encoding == "br" {
		compressed := brotli.NewWriterLevel(writer, 5)
		_, err := io.Copy(compressed, source)
		cleanup.Error("write MAME Brotli response", err)
		cleanup.Error("finish MAME Brotli response", compressed.Close())
		return true
	}
	compressed := gzip.NewWriter(writer)
	_, err := io.Copy(compressed, source)
	cleanup.Error("write MAME gzip response", err)
	cleanup.Error("finish MAME gzip response", compressed.Close())
	return true
}

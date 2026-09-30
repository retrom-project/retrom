package httpapi

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"retrom/internal/cleanup"
)

// Whole consumers verify decoded SHA-256. Conditional and Range consumers keep
// the original strong validator, exact lengths and byte offsets.
func serveRuntimeContent(
	writer http.ResponseWriter, request *http.Request, name string, modified time.Time, body io.ReadSeeker,
) {
	size, err := body.Seek(0, io.SeekEnd)
	if err == nil && size >= 4096 {
		writer.Header().Add("Vary", "Accept-Encoding")
	}
	if err != nil || size < 4096 || request.Method != http.MethodGet || request.Header.Get("Range") != "" ||
		request.Header.Get("If-Match") != "" || request.Header.Get("If-Range") != "" ||
		!acceptsContentGzip(request.Header.Get("Accept-Encoding")) {
		http.ServeContent(writer, request, name, modified, body)
		return
	}
	compressed, err := gzip.NewWriterLevel(writer, gzip.BestSpeed)
	if err != nil {
		http.ServeContent(writer, request, name, modified, body)
		return
	}
	etag := writer.Header().Get("ETag")
	writer.Header().Set("ETag", `W/"`+strings.Trim(etag, `"`)+`-gzip"`)
	writer.Header().Set("Content-Encoding", "gzip")
	response := &compressedContentWriter{ResponseWriter: writer, compressed: compressed}
	http.ServeContent(response, request, name, modified, body)
	if response.hasBody {
		cleanup.Error("finish compressed runtime content", compressed.Close())
	}
}

type compressedContentWriter struct {
	http.ResponseWriter
	compressed *gzip.Writer
	hasBody    bool
}

func (writer *compressedContentWriter) WriteHeader(status int) {
	writer.hasBody = status == http.StatusOK
	if writer.hasBody {
		writer.Header().Del("Content-Length")
		writer.Header().Del("Accept-Ranges")
	} else if status >= 400 {
		writer.Header().Del("Content-Encoding")
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *compressedContentWriter) Write(bytes []byte) (int, error) {
	if writer.hasBody {
		written, err := writer.compressed.Write(bytes)
		if err != nil {
			return written, fmt.Errorf("write compressed runtime content: %w", err)
		}
		return written, nil
	}
	written, err := writer.ResponseWriter.Write(bytes)
	if err != nil {
		return written, fmt.Errorf("write runtime content response: %w", err)
	}
	return written, nil
}

func acceptsContentGzip(header string) bool {
	wildcard := false
	for _, entry := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(entry), ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		if name != "gzip" && name != "*" {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, value, exists := strings.Cut(strings.TrimSpace(parameter), "=")
			if !exists || !strings.EqualFold(key, "q") {
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
		if name == "gzip" {
			return quality > 0
		}
		wildcard = quality > 0
	}
	return wildcard
}

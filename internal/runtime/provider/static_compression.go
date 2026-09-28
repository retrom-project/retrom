package runtimeprovider

import (
	"net/http"
	"strconv"
	"strings"
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
	wildcard := false
	for _, entry := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(entry), ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		if name != "br" && name != "*" {
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
		if name == "br" {
			return quality > 0
		}
		wildcard = quality > 0
	}
	return wildcard
}

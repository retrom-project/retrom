package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"retrom/internal/launch"
)

const tyranoScriptBlackJPEGBase64 = "" +
	"/9j/2wBDAAYEBQYFBAYGBQYHBwYIChAKCgkJChQODwwQFxQYGBcUFhYaHSUfGhsjHBYWICwgIyYnKSopGR8t" +
	"MC0oMCUoKSj/2wBDAQcHBwoIChMKChMoGhYaKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgo" +
	"KCgoKCgoKCgoKCj/wAARCAABAAEDASIAAhEBAxEB/8QAFQABAQAAAAAAAAAAAAAAAAAAAAj/xAAUEAEAAAAAAAA" +
	"AAAAAAAAAAAAA/8QAFAEBAAAAAAAAAAAAAAAAAAAAAP/EABQRAQAAAAAAAAAAAAAAAAAAAAD/2gAMAwEAAhEDEQA/" +
	"AJUAB//Z"

var tyranoScriptFileStorage = regexp.MustCompile(
	`(?m)^([\t ]*;configSave[\t ]*=[\t ]*)file([\t ]*\r?)$`,
)

func projectTyranoScriptConfig(contents []byte) ([]byte, error) {
	if !utf8.Valid(contents) {
		return nil, errRPGEntryUTF8
	}
	return tyranoScriptFileStorage.ReplaceAll(contents, []byte(`${1}webstorage${2}`)), nil
}

func (server *Server) serveTyranoScriptConfig(
	writer http.ResponseWriter,
	request *http.Request,
	content launch.ContentView,
) {
	original, err := server.readRPGContent(content, maxNativeEntryBytes)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable,
			"TYRANOSCRIPT_RUNTIME_CONTENT_MISMATCH", "TyranoScript 配置不可用", map[string]any{})
		return
	}
	projected, err := projectTyranoScriptConfig(original)
	if err != nil {
		writeError(writer, request, http.StatusUnprocessableEntity,
			"TYRANOSCRIPT_CONFIG_UNSUPPORTED", "TyranoScript 配置无法安全转换", map[string]any{})
		return
	}
	digest := sha256.Sum256(projected)
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	writer.Header().Set("ETag", `"sha256-`+hex.EncodeToString(digest[:])+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, "Config.tjs", time.Unix(0, 0), bytes.NewReader(projected))
}

func serveTyranoScriptVirtualAsset(
	writer http.ResponseWriter,
	request *http.Request,
	logicalName string,
) bool {
	// Tyrano 4.x projects commonly use black.jpg as an implicit transition backdrop while
	// relying on NW.js' black window background when the physical asset is omitted.
	if !strings.EqualFold(logicalName, "data/bgimage/black.jpg") {
		return false
	}
	contents, err := base64.StdEncoding.DecodeString(tyranoScriptBlackJPEGBase64)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(contents)
	writer.Header().Set("Content-Type", "image/jpeg")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	writer.Header().Set("ETag", `"sha256-`+hex.EncodeToString(digest[:])+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, "black.jpg", time.Unix(0, 0), bytes.NewReader(contents))
	return true
}

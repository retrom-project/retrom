package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"retrom/internal/cleanup"
	"retrom/internal/launch"
	"retrom/internal/rpgmaker/isolation"
	"retrom/internal/rpgmaker/nativeweb"
	"retrom/internal/saves"
)

const maxNativeEntryBytes = 2 << 20

var (
	errRPGEntryTooLarge      = errors.New("RPG entry exceeds limit")
	errRPGEntryDigest        = errors.New("RPG entry digest mismatch")
	errRPGEntryUTF8          = errors.New("RPG entry is not UTF-8")
	errRPGEntryHeadMissing   = errors.New("RPG entry has no head")
	errRPGEntryHeadMalformed = errors.New("RPG entry head is malformed")
	errRPGBaseMalformed      = errors.New("RPG base element is malformed")
)

const rpgBootstrapDocument = `<!doctype html>
<meta charset="utf-8">
<script nonce="%s">
(()=>{"use strict";
const p=%s;
let used=false;
parent.postMessage({type:"RPG_RUNTIME_NATIVE_BOOTSTRAP_READY",protocolVersion:1},p);
addEventListener("message",async e=>{
  const d=e.data;
  if(used||e.source!==parent||e.origin!==p||!d||typeof d!=="object"||
    Object.keys(d).sort().join(",")!=="protocolVersion,ticket,type"||
    d.type!=="RPG_RUNTIME_NATIVE_BOOTSTRAP"||d.protocolVersion!==1||typeof d.ticket!=="string")return;
  used=true;
  try{
    const r=await fetch("/__retrom/bootstrap",{method:"POST",credentials:"same-origin",
      headers:{"Content-Type":"application/json"},body:JSON.stringify({ticket:d.ticket})});
    if(!r.ok)throw new Error();
    location.replace("/__retrom/entry");
  }catch(_){
    parent.postMessage({type:"RPG_RUNTIME_NATIVE_BOOTSTRAP_FAILED",protocolVersion:1},p);
  }
});
})()
</script>`

func (server *Server) rpgBootstrapPage(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	setRPGFrameDocumentPolicy(writer)
	if _, err := server.authenticateRPGRuntime(request, access); err == nil {
		writer.Header().Set("Cache-Control", "private, no-store")
		http.Redirect(writer, request, "/__retrom/entry", http.StatusSeeOther)
		return
	}
	if _, err := server.rpgIsolation.InspectBootstrap(request.Context(), access.LaunchID, access.Origin); err != nil {
		writeError(writer, request, http.StatusGone, "RPG_RUNTIME_BOOTSTRAP_EXPIRED", "RPG Maker 启动凭据已过期", map[string]any{})
		return
	}
	nonce, err := rpgRuntimeNonce()
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	parentOrigin, _ := json.Marshal(server.config.PublicOrigin.String())
	body := fmt.Sprintf(rpgBootstrapDocument, html.EscapeString(nonce), parentOrigin)
	setRPGBootstrapCSP(writer, nonce, server.config.PublicOrigin.String())
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(writer, body)
}

func (server *Server) rpgBootstrapConsume(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	if !validRPGRuntimeWrite(request, access.Origin) {
		http.NotFound(writer, request)
		return
	}
	var body struct {
		Ticket string `json:"ticket"`
	}
	if err := decodeJSON(writer, request, &body, 256); err != nil {
		writeError(
			writer, request, http.StatusBadRequest,
			"RPG_RUNTIME_PROTOCOL_VIOLATION", "RPG Maker 启动请求无效", map[string]any{},
		)
		return
	}
	credential, consumed, err := server.rpgIsolation.ConsumeTicket(
		request.Context(), access.LaunchID, access.Origin, body.Ticket,
	)
	if err != nil {
		writeError(writer, request, http.StatusGone, "RPG_RUNTIME_BOOTSTRAP_EXPIRED", "RPG Maker 启动凭据已过期", map[string]any{})
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name: rpgRuntimeCookieName, Value: credential, Path: "/__retrom/", HttpOnly: true,
		Secure: strings.HasPrefix(consumed.Origin, "https://"), SameSite: http.SameSiteStrictMode,
		Expires: time.UnixMilli(consumed.Expires), MaxAge: int(time.Until(time.UnixMilli(consumed.Expires)).Seconds()),
	})
	writer.Header().Set("Clear-Site-Data", `"storage"`)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) rpgRuntimeCleanup(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	authorized, err := server.authenticateRPGRuntime(request, access)
	if err != nil || !validRPGRuntimeWrite(request, access.Origin) ||
		server.rpgIsolation.Revoke(request.Context(), authorized) != nil {
		http.NotFound(writer, request)
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name: rpgRuntimeCookieName, Path: "/__retrom/", HttpOnly: true,
		Secure: strings.HasPrefix(access.Origin, "https://"), SameSite: http.SameSiteStrictMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
	writer.Header().Set("Clear-Site-Data", `"storage"`)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) rpgRuntimeEntry(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	setRPGFrameDocumentPolicy(writer)
	if _, err := server.authenticateRPGRuntime(request, access); err != nil {
		http.NotFound(writer, request)
		return
	}
	content, err := server.launcher.ContentAuthorized(request.Context(), access.LaunchID, "index.html")
	if err != nil || content.Format != "RPG_MAKER_PROJECT_V1" {
		http.NotFound(writer, request)
		return
	}
	original, err := server.readRPGContent(content, maxNativeEntryBytes)
	if err != nil {
		writeError(
			writer, request, http.StatusServiceUnavailable,
			"RPG_RUNTIME_CONTENT_MISMATCH", "RPG Maker 入口不可用", map[string]any{},
		)
		return
	}
	entry, err := transformRPGEntry(original)
	if err != nil {
		writeError(
			writer, request, http.StatusUnprocessableEntity,
			"RPG_NATIVE_BRIDGE_UNSUPPORTED", "RPG Maker 入口无法安全转换", map[string]any{},
		)
		return
	}
	nonce, err := rpgRuntimeNonce()
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	setRPGEntryCSP(writer, nonce, server.config.PublicOrigin.String())
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Content-Length", fmt.Sprint(len(entry)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(entry)
}

func (server *Server) rpgRuntimeBridge(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	if _, err := server.authenticateRPGRuntime(request, access); err != nil {
		http.NotFound(writer, request)
		return
	}
	runtimeVersion, bridgePath, err := server.launcher.RPGNativeBridgeAuthorized(request.Context(), access.LaunchID)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	runtimePath, declaration, ok := server.dependencies.RetromRuntimeFile(runtimeVersion, bridgePath)
	if !ok {
		writeError(
			writer, request, http.StatusServiceUnavailable,
			"DEPENDENCY_INVALID", "RPG Maker bridge 不可用", map[string]any{},
		)
		return
	}
	server.serveRPGDependency(
		writer, request, runtimePath, declaration.SizeBytes, declaration.SHA256,
		"application/javascript; charset=utf-8",
	)
}

func (server *Server) rpgRuntimeProject(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	if _, err := server.authenticateRPGRuntime(request, access); err != nil {
		http.NotFound(writer, request)
		return
	}
	if isRPGServiceWorkerRequest(request) {
		http.NotFound(writer, request)
		return
	}
	logicalName := strings.TrimPrefix(request.URL.Path, "/__retrom/project/")
	if !validNativeProjectPath(logicalName) {
		http.NotFound(writer, request)
		return
	}
	mediaType, allowed := nativeProjectMIME(logicalName)
	if !allowed {
		http.NotFound(writer, request)
		return
	}
	content, err := server.launcher.RPGProjectContentAuthorized(request.Context(), access.LaunchID, logicalName)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	server.serveRPGBlob(writer, request, content.Digest, mediaType)
}

func isRPGServiceWorkerRequest(request *http.Request) bool {
	return strings.EqualFold(request.Header.Get("Sec-Fetch-Dest"), "serviceworker")
}

func (server *Server) rpgRuntimeRestorePayload(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	if _, err := server.authenticateRPGRuntime(request, access); err != nil {
		http.NotFound(writer, request)
		return
	}
	digest, err := server.saveService.IsolatedStateDigest(request.Context(), access.LaunchID)
	if err != nil {
		status := http.StatusConflict
		code := "RPG_CHECKPOINT_INCOMPATIBLE"
		if errors.Is(err, saves.ErrCheckpointInvalid) {
			code = "RPG_CHECKPOINT_INVALID"
		}
		writeError(writer, request, status, code, "RPG Maker 恢复数据不可用", map[string]any{})
		return
	}
	server.serveRPGBlob(writer, request, digest, "application/octet-stream")
}

func (server *Server) readRPGContent(content launch.ContentView, maximum int64) ([]byte, error) {
	file, err := server.blobs.OpenDigest(content.Digest)
	if err != nil {
		return nil, fmt.Errorf("open RPG entry: %w", err)
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	contents, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(contents)) > maximum {
		return nil, errRPGEntryTooLarge
	}
	digest := sha256.Sum256(contents)
	if hex.EncodeToString(digest[:]) != content.Digest {
		return nil, errRPGEntryDigest
	}
	return contents, nil
}

func (server *Server) serveRPGBlob(
	writer http.ResponseWriter,
	request *http.Request,
	digest, mediaType string,
) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	file, err := server.blobs.OpenDigest(digest)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "RPG Maker 内容不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	writer.Header().Set("ETag", `"sha256-`+digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, "content", time.Unix(0, 0), file)
}

func (server *Server) serveRPGDependency(
	writer http.ResponseWriter,
	request *http.Request,
	runtimePath string,
	expectedSize int64,
	digest, mediaType string,
) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	file, err := os.Open(runtimePath)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != expectedSize {
		writeError(
			writer, request, http.StatusServiceUnavailable,
			"DEPENDENCY_INVALID", "RPG Maker bridge 不可用", map[string]any{},
		)
		return
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("ETag", `"sha256-`+digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, filepath.Base(runtimePath), time.Unix(0, 0), file)
}

func transformRPGEntry(contents []byte) ([]byte, error) {
	if !utf8.Valid(contents) {
		return nil, errRPGEntryUTF8
	}
	lowered := bytes.ToLower(contents)
	headStart := bytes.Index(lowered, []byte("<head"))
	if headStart < 0 {
		return nil, errRPGEntryHeadMissing
	}
	headEnd := htmlTagEnd(contents, headStart+5)
	if headEnd < 0 {
		return nil, errRPGEntryHeadMalformed
	}
	withoutBase, err := removeRPGBaseElements(contents)
	if err != nil {
		return nil, err
	}
	lowered = bytes.ToLower(withoutBase)
	headStart = bytes.Index(lowered, []byte("<head"))
	headEnd = htmlTagEnd(withoutBase, headStart+5)
	if headStart < 0 || headEnd < 0 {
		return nil, errRPGEntryHeadMalformed
	}
	injection := []byte(`<base href="/__retrom/project/"><script src="/__retrom/bridge.js"></script>`)
	result := make([]byte, 0, len(withoutBase)+len(injection))
	result = append(result, withoutBase[:headEnd+1]...)
	result = append(result, injection...)
	result = append(result, withoutBase[headEnd+1:]...)
	return result, nil
}

func removeRPGBaseElements(contents []byte) ([]byte, error) {
	result := make([]byte, 0, len(contents))
	lowered := bytes.ToLower(contents)
	position := 0
	for {
		relative := bytes.Index(lowered[position:], []byte("<base"))
		if relative < 0 {
			result = append(result, contents[position:]...)
			return result, nil
		}
		start := position + relative
		afterName := start + len("<base")
		if afterName < len(contents) && !strings.ContainsRune(" \t\r\n/>", rune(contents[afterName])) {
			result = append(result, contents[position:afterName]...)
			position = afterName
			continue
		}
		end := htmlTagEnd(contents, afterName)
		if end < 0 {
			return nil, errRPGBaseMalformed
		}
		result = append(result, contents[position:start]...)
		position = end + 1
	}
}

func htmlTagEnd(contents []byte, position int) int {
	quote := byte(0)
	for ; position < len(contents); position++ {
		current := contents[position]
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}
		switch current {
		case '\'', '"':
			quote = current
		case '>':
			return position
		}
	}
	return -1
}

func validNativeProjectPath(value string) bool {
	return value != "" && len(value) <= 512 && utf8.ValidString(value) && path.Clean(value) == value &&
		!path.IsAbs(value) && value != "." && !strings.HasPrefix(value, "../") &&
		!strings.Contains(value, "\\") && !strings.ContainsRune(value, 0)
}

func nativeProjectMIME(logicalName string) (string, bool) {
	return nativeweb.ProjectMediaType(logicalName)
}

func setRPGBootstrapCSP(writer http.ResponseWriter, nonce, parentOrigin string) {
	value := "default-src 'none'; script-src 'nonce-" + nonce + "'; connect-src 'self'; " +
		"base-uri 'none'; form-action 'none'; frame-ancestors " + parentOrigin
	writer.Header().Set("Content-Security-Policy", value)
}

func setRPGFrameDocumentPolicy(writer http.ResponseWriter) {
	writer.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
}

func setRPGEntryCSP(writer http.ResponseWriter, nonce, parentOrigin string) {
	value := "default-src 'self' data: blob:; script-src 'self' 'nonce-" + nonce + "' 'unsafe-eval' blob:; " +
		"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' data: blob:; " +
		"font-src 'self' data: blob:; connect-src 'self'; worker-src 'self' blob:; frame-src 'none'; " +
		"object-src 'none'; base-uri 'self'; form-action 'none'; frame-ancestors " + parentOrigin
	writer.Header().Set("Content-Security-Policy", value)
}

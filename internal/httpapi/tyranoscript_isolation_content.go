package httpapi

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"path"
	"strings"

	"retrom/internal/rpgmaker/isolation"
)

const tyranoScriptBootstrapDocument = `<!doctype html>
<meta charset="utf-8">
<script nonce="%s">
(()=>{"use strict";
const p=%s;
let used=false;
parent.postMessage({type:"GAME_RUNTIME_TYRANOSCRIPT_BOOTSTRAP_REQUIRED",protocolVersion:1},p);
addEventListener("message",async e=>{
  const d=e.data;
  if(used||e.source!==parent||e.origin!==p||!d||typeof d!=="object"||
    Object.keys(d).sort().join(",")!=="protocolVersion,ticket,type"||
    d.type!=="GAME_RUNTIME_TYRANOSCRIPT_BOOTSTRAP"||d.protocolVersion!==1||typeof d.ticket!=="string")return;
  used=true;
  try{
    const r=await fetch("/__retrom/tyranoscript/bootstrap",{method:"POST",credentials:"same-origin",
      headers:{"Content-Type":"application/json"},body:JSON.stringify({ticket:d.ticket})});
    if(!r.ok)throw new Error();
    location.replace("/__retrom/tyranoscript/entry");
  }catch(_){
    parent.postMessage({type:"GAME_RUNTIME_TYRANOSCRIPT_BOOTSTRAP_FAILED",protocolVersion:1},p);
  }
});
})()
</script>`

func (server *Server) tyranoScriptBootstrapPage(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	setRPGFrameDocumentPolicy(writer)
	if authorized, err := server.authenticateRPGRuntime(request, access); err == nil &&
		authorized.Family == "TYRANOSCRIPT" {
		writer.Header().Set("Cache-Control", "private, no-store")
		http.Redirect(writer, request, "/__retrom/tyranoscript/entry", http.StatusSeeOther)
		return
	}
	inspected, err := server.rpgIsolation.InspectBootstrap(request.Context(), access.LaunchID, access.Origin)
	if err != nil || inspected.Family != "TYRANOSCRIPT" {
		http.NotFound(writer, request)
		return
	}
	nonce, err := rpgRuntimeNonce()
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	parentOrigin, _ := json.Marshal(server.config.PublicOrigin.String())
	body := fmt.Sprintf(tyranoScriptBootstrapDocument, html.EscapeString(nonce), parentOrigin)
	setRPGBootstrapCSP(writer, nonce, server.config.PublicOrigin.String())
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(writer, body)
}

func (server *Server) tyranoScriptBootstrapConsume(
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
		http.NotFound(writer, request)
		return
	}
	credential, consumed, err := server.rpgIsolation.ConsumeTicket(
		request.Context(), access.LaunchID, access.Origin, body.Ticket,
	)
	if err != nil || consumed.Family != "TYRANOSCRIPT" {
		http.NotFound(writer, request)
		return
	}
	setIsolatedRuntimeCookie(writer, consumed, credential)
	writer.Header().Set("Clear-Site-Data", `"storage"`)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) tyranoScriptRuntimeEntry(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	setRPGFrameDocumentPolicy(writer)
	authorized, err := server.authenticateRPGRuntime(request, access)
	if err != nil || authorized.Family != "TYRANOSCRIPT" {
		http.NotFound(writer, request)
		return
	}
	content, err := server.launcher.TyranoScriptProjectContentAuthorized(
		request.Context(), access.LaunchID, "index.html", authorized.Preview,
	)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	original, err := server.readRPGContent(content, maxNativeEntryBytes)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable,
			"TYRANOSCRIPT_RUNTIME_CONTENT_MISMATCH", "TyranoScript 入口不可用", map[string]any{})
		return
	}
	entry, err := transformIsolatedEntry(
		original, "/__retrom/tyranoscript/project/", "/__retrom/tyranoscript/bridge.js",
	)
	if err != nil {
		writeError(writer, request, http.StatusUnprocessableEntity,
			"TYRANOSCRIPT_ENTRY_UNSUPPORTED", "TyranoScript 入口无法安全转换", map[string]any{})
		return
	}
	setTyranoScriptEntryCSP(writer, server.config.PublicOrigin.String())
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Content-Length", fmt.Sprint(len(entry)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(entry)
}

func (server *Server) tyranoScriptRuntimeBridge(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	authorized, err := server.authenticateRPGRuntime(request, access)
	if err != nil || authorized.Family != "TYRANOSCRIPT" {
		http.NotFound(writer, request)
		return
	}
	runtimeVersion, bridgePath, err := server.launcher.TyranoScriptBridgeAuthorized(
		request.Context(), access.LaunchID, authorized.Preview,
	)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	runtimePath, declaration, ok := server.dependencies.RetromRuntimeFile(runtimeVersion, bridgePath)
	if !ok {
		writeError(writer, request, http.StatusServiceUnavailable,
			"DEPENDENCY_INVALID", "TyranoScript bridge 不可用", map[string]any{})
		return
	}
	server.serveRPGDependency(
		writer, request, runtimePath, declaration.SizeBytes, declaration.SHA256,
		"application/javascript; charset=utf-8",
	)
}

func (server *Server) tyranoScriptRuntimeProject(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	authorized, err := server.authenticateRPGRuntime(request, access)
	if err != nil || authorized.Family != "TYRANOSCRIPT" || isRPGServiceWorkerRequest(request) {
		http.NotFound(writer, request)
		return
	}
	logicalName, valid := tyranoScriptProjectLogicalName(request.URL.Path)
	if !valid {
		http.NotFound(writer, request)
		return
	}
	mediaType, allowed := tyranoScriptProjectMIME(logicalName)
	if !allowed {
		http.NotFound(writer, request)
		return
	}
	content, err := server.launcher.TyranoScriptProjectContentAuthorized(
		request.Context(), access.LaunchID, logicalName, authorized.Preview,
	)
	if err != nil {
		if serveTyranoScriptVirtualAsset(writer, request, logicalName) {
			return
		}
		http.NotFound(writer, request)
		return
	}
	if strings.EqualFold(logicalName, "data/system/Config.tjs") {
		server.serveTyranoScriptConfig(writer, request, content)
		return
	}
	server.serveRPGBlob(writer, request, content.Digest, mediaType)
}

func tyranoScriptProjectLogicalName(requestPath string) (string, bool) {
	const projectPrefix = "/__retrom/tyranoscript/project/"
	const enginePrefix = "/__retrom/tyranoscript/"
	logicalName := ""
	switch {
	case strings.HasPrefix(requestPath, projectPrefix):
		logicalName = strings.TrimPrefix(requestPath, projectPrefix)
	case strings.HasPrefix(requestPath, enginePrefix):
		logicalName = strings.TrimPrefix(requestPath, enginePrefix)
		if !tyranoScriptEngineOwnedPath(logicalName) {
			return "", false
		}
	case strings.HasPrefix(requestPath, "/") && !strings.HasPrefix(requestPath, "/__retrom/"):
		logicalName = strings.TrimPrefix(requestPath, "/")
		if !tyranoScriptEngineOwnedPath(logicalName) {
			return "", false
		}
	}
	if !validNativeProjectPath(logicalName) {
		return "", false
	}
	if _, allowed := tyranoScriptProjectMIME(logicalName); !allowed {
		return "", false
	}
	return logicalName, true
}

func tyranoScriptEngineOwnedPath(logicalName string) bool {
	return strings.HasPrefix(logicalName, "data/") || strings.HasPrefix(logicalName, "tyrano/")
}

func tyranoScriptProjectMIME(logicalName string) (string, bool) {
	mediaTypes := map[string]string{
		".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
		".js": "application/javascript; charset=utf-8", ".mjs": "application/javascript; charset=utf-8",
		".css": "text/css; charset=utf-8", ".json": "application/json; charset=utf-8",
		".ks": "text/plain; charset=utf-8", ".tjs": "text/plain; charset=utf-8",
		".txt": "text/plain; charset=utf-8", ".csv": "text/csv; charset=utf-8",
		".xml": "application/xml; charset=utf-8", ".svg": "image/svg+xml",
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".webp": "image/webp", ".gif": "image/gif", ".bmp": "image/bmp", ".ico": "image/x-icon",
		".ogg": "audio/ogg", ".opus": "audio/ogg", ".m4a": "audio/mp4",
		".mp3": "audio/mpeg", ".wav": "audio/wav", ".mp4": "video/mp4", ".webm": "video/webm",
		".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf",
		".otf": "font/otf", ".eot": "application/vnd.ms-fontobject", ".bin": "application/octet-stream",
	}
	mediaType, exists := mediaTypes[strings.ToLower(path.Ext(logicalName))]
	return mediaType, exists
}

func setTyranoScriptEntryCSP(writer http.ResponseWriter, parentOrigin string) {
	value := "default-src 'self' data: blob:; script-src 'self' 'unsafe-inline' 'unsafe-eval' blob:; " +
		"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' data: blob:; " +
		"font-src 'self' data: blob:; connect-src 'self'; worker-src 'self' blob:; frame-src 'none'; " +
		"object-src 'none'; base-uri 'self'; form-action 'none'; frame-ancestors " + parentOrigin
	writer.Header().Set("Content-Security-Policy", value)
}

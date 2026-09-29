package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/launch"
)

// Native programs are data on the app origin and execute only on an isolated origin.
func (server *Server) launchWebContent(writer http.ResponseWriter, request *http.Request) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	grant, valid, err := server.runtimeProjectContentGrant(request, request.PathValue("contentIdentity"))
	if err != nil {
		server.runtimeContentUnavailable(writer, request, err)
		return
	}
	if !valid {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "项目内容不可用", map[string]any{})
		return
	}
	name := request.PathValue("projectPath")
	if name == "index.json" {
		server.serveNativeProjectIndex(writer, request, grant)
		return
	}
	if !strings.HasPrefix(name, "files/") {
		http.NotFound(writer, request)
		return
	}
	name = strings.TrimPrefix(name, "files/")
	if !validNativeProjectPath(name) {
		http.NotFound(writer, request)
		return
	}
	content, err := server.projectContent(request, grant.LaunchID, grant.Capability, name)
	if err != nil && !errors.Is(err, launch.ErrCredential) {
		server.runtimeContentUnavailable(writer, request, err)
		return
	}
	if err != nil || content.DeliveryProfile != "ISOLATED_WEB_PROJECT" ||
		(content.Format != "RPG_MAKER_PROJECT" && content.Format != "TYRANOSCRIPT_PROJECT") {
		http.NotFound(writer, request)
		return
	}
	if _, allowed := webProjectMediaType(content.Format, name); !allowed {
		http.NotFound(writer, request)
		return
	}
	file, err := server.contentDeps.Files.OpenRecord(content.FileRecord)
	if err != nil {
		writer.Header().Set("Retry-After", "1")
		writeError(writer, request, http.StatusServiceUnavailable, "FILE_STORAGE_UNAVAILABLE", "项目内容不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	setWebContentDownloadHeaders(writer)
	writer.Header().Set("ETag", `"sha256-`+content.Digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, "content", time.Unix(0, 0), file)
}

func setWebContentDownloadHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Disposition", `attachment; filename="content"`)
	writer.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	writer.Header().Set("Cache-Control", immutablePrivateContent)
}

func webProjectMediaType(format, name string) (string, bool) {
	if name == "index.html" {
		return "text/html; charset=utf-8", true
	}
	if format == "RPG_MAKER_PROJECT" {
		return nativeProjectMIME(name)
	}
	return tyranoScriptProjectMIME(name)
}

func (server *Server) serveNativeProjectIndex(
	writer http.ResponseWriter, request *http.Request, grant runtimeContentGrant,
) {
	index, err := server.playDeps.Launcher.ProjectIndex(request.Context(), grant.LaunchID, grant.Capability)
	if err != nil && !errors.Is(err, launch.ErrCredential) && !errors.Is(err, launch.ErrProjectIndexUnavailable) {
		server.runtimeContentUnavailable(writer, request, err)
		return
	}
	if !server.writeGeneratedProjectIndex(writer, request, index, err) {
		http.NotFound(writer, request)
	}
}

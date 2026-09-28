package httpapi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"retrom/internal/service/isolation"
)

//go:embed web_content/bridge.js
var webContentBridge string

//go:embed web_content/rpc.js
var webContentRPC string

//go:embed web_content/response.js
var webContentResponse string

//go:embed web_content/worker.js
var webContentWorker string

const (
	webContentBootstrapPath = "/__retrom/content-bootstrap"
	webContentWorkerPath    = "/__retrom/content-worker.js"
	webContentBridgePath    = "/__retrom/content-bridge.js"
)

type webContentConfiguration struct {
	Parent      string `json:"parent"`
	Bootstrap   string `json:"bootstrap"`
	Entry       string `json:"entry"`
	Project     string `json:"project"`
	Bridge      string `json:"bridge"`
	CSP         string `json:"csp"`
	Permissions string `json:"permissions"`
	Tyrano      bool   `json:"tyrano"`
	BlackJPEG   string `json:"blackJPEG,omitempty"`
}

func nativeContentConfiguration(format, parent string) webContentConfiguration {
	configuration := webContentConfiguration{
		Parent: parent, Bootstrap: webContentBootstrapPath,
		Entry: "/__retrom/entry", Project: "/__retrom/project/", Bridge: "/__retrom/bridge.js",
		Permissions: rpgRuntimePermissionsPolicy,
		CSP: "default-src 'self' data: blob:; script-src 'self' 'unsafe-eval' blob:; " +
			"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' data: blob:; " +
			"font-src 'self' data: blob:; connect-src 'self'; worker-src 'self' blob:; frame-src 'none'; " +
			"object-src 'none'; base-uri 'self'; form-action 'none'; frame-ancestors " + parent,
	}
	if format == "TYRANOSCRIPT_PROJECT" {
		configuration.Tyrano = true
		configuration.Entry, configuration.Project = "/__retrom/tyranoscript/entry", "/__retrom/tyranoscript/project/"
		configuration.Bridge = "/__retrom/tyranoscript/bridge.js"
		configuration.CSP = strings.Replace(configuration.CSP, "script-src 'self'", "script-src 'self' 'unsafe-inline'", 1)
		configuration.BlackJPEG = tyranoScriptBlackJPEGBase64
	}
	return configuration
}

func (server *Server) serveWebContentTransport(
	writer http.ResponseWriter, request *http.Request, access isolation.Access,
) {
	authorized, err := server.authenticateRPGRuntime(request, access)
	if err != nil ||
		(authorized.ContentFormat != "RPG_MAKER_PROJECT" && authorized.ContentFormat != "TYRANOSCRIPT_PROJECT") {
		http.NotFound(writer, request)
		return
	}
	config := nativeContentConfiguration(authorized.ContentFormat, server.config.PublicOrigin.String())
	writer.Header().Set("Cache-Control", "private, no-store")
	switch request.URL.Path {
	case webContentBootstrapPath:
		server.serveWebContentBootstrap(writer, config)
	case webContentBridgePath:
		writer.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = io.WriteString(writer, webContentBridge)
	case webContentWorkerPath:
		value, err := json.Marshal(config)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		writer.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		writer.Header().Set("Service-Worker-Allowed", "/__retrom/")
		script := "const configuration=" + string(value) + ";\n" + webContentRPC + webContentResponse + webContentWorker
		_, _ = io.WriteString(writer, script)
	default:
		http.NotFound(writer, request)
	}
}

func (server *Server) serveWebContentBootstrap(writer http.ResponseWriter, config webContentConfiguration) {
	setRPGFrameDocumentPolicy(writer)
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; worker-src 'self'; "+
		"connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors "+config.Parent)
	const document = `<!doctype html><html><head><meta charset="utf-8"></head><body>` +
		`<script src="%s" data-parent="%s" data-entry="%s" data-worker="%s"></script></body></html>`
	_, _ = fmt.Fprintf(writer, document,
		webContentBridgePath, html.EscapeString(config.Parent), config.Entry, webContentWorkerPath)
}

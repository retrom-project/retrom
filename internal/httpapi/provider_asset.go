package httpapi

import (
	"net/http"

	"retrom/internal/launch"
)

func (server *Server) serveProviderAsset(
	writer http.ResponseWriter,
	request *http.Request,
	asset launch.ProviderAsset,
) {
	forwarded := request.Clone(request.Context())
	forwarded.URL.Path = "/runtime/providers/" + asset.ProviderID + "/" + asset.BundleSHA256 + "/" + asset.Path
	forwarded.SetPathValue("providerId", asset.ProviderID)
	forwarded.SetPathValue("bundleSha256", asset.BundleSHA256)
	forwarded.SetPathValue("runtimePath", asset.Path)
	server.runtimeProvider.ServeHTTP(writer, forwarded)
}

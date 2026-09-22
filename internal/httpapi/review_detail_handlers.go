package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	libraryservice "retrom/internal/service/libraryimport"
)

func (server *Server) review(writer http.ResponseWriter, request *http.Request) {
	result, err := server.reviewDetails.Get(request.Context(), request.PathValue("importItemId"))
	if errors.Is(err, libraryservice.ErrReviewNotFound) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	projectReviewDetailURLs(&result)
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(writer, http.StatusOK, result)
}

func projectReviewDetailURLs(result *libraryservice.ReviewDetail) {
	for i := range result.UploadedAssets {
		asset := &result.UploadedAssets[i]
		asset.URL = "/api/v1/admin/review-assets/" + asset.ID
	}
	if result.RuntimeScreenshot != nil {
		result.RuntimeScreenshot.URL = "/api/v1/admin/review-assets/" + result.RuntimeScreenshot.ID
	}
	if result.ArcadeDependencies != nil {
		for i := range result.ArcadeDependencies.Nodes {
			node := &result.ArcadeDependencies.Nodes[i]
			if node.Kind == "BIOS_OR_BASE" {
				node.ManagementURL = "/admin/bios"
			}
		}
	}
	projectReviewSourceMediaURLs(result.SourceMedia)
}

func projectReviewSourceMediaURLs(media *libraryservice.ReviewSourceMedia) {
	if media == nil {
		return
	}
	media.SourceImportID = media.ImportID
	base := "/api/v1/admin/review-assets/" + media.SourceRefID
	if media.HasCover {
		value := base + "?kind=COVER"
		media.CoverURL = &value
	}
	if media.HasVideo {
		value := base + "?kind=VIDEO"
		media.VideoURL = &value
	}
}

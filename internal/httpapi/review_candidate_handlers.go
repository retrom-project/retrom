package httpapi

import (
	"fmt"
	"net/http"

	"retrom/internal/service/metadatascrape"
)

func (server *Server) reviewCandidateAssets(
	request *http.Request,
	candidateID string,
) ([]metadatascrape.CandidateAssetView, error) {
	assets, err := server.reviewDeps.Evidence.Assets(request.Context(), []string{candidateID})
	if err != nil {
		return nil, fmt.Errorf("read review candidate assets: %w", err)
	}
	return assets, nil
}

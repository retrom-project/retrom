package httpapi

import (
	"fmt"
	"net/http"

	"retrom/internal/model/metadatascrape"
)

func (server *Server) reviewCandidateAssets(
	request *http.Request,
	candidateID string,
) ([]metadatascrape.CandidateAssetView, error) {
	assets, err := server.metadataEvidence.Assets(request.Context(), []string{candidateID})
	if err != nil {
		return nil, fmt.Errorf("read review candidate assets: %w", err)
	}
	return assets, nil
}

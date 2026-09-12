package httpapi

import (
	"fmt"
	"net/http"

	"retrom/internal/dbexec"
	"retrom/internal/service/metadatascrape"
)

type rowScanner = dbexec.Scanner

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

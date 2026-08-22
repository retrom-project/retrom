package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"retrom/internal/storageanalysis"
)

var errStorageAnalysisDatabaseMissing = errors.New("storage analysis read-only database missing")

type storageAnalysisResponse struct {
	Scope         string                    `json:"scope"`
	GeneratedAtMS int64                     `json:"generatedAtMs"`
	Totals        storageAnalysisTotals     `json:"totals"`
	Categories    []storageAnalysisCategory `json:"categories"`
	Details       storageAnalysisDetails    `json:"details"`
	Excluded      []string                  `json:"excluded"`
}

type storageAnalysisTotals struct {
	RegisteredBytes   string `json:"registeredBytes"`
	ProtectedBytes    string `json:"protectedBytes"`
	UnreferencedBytes string `json:"unreferencedBytes"`
	BlobCount         int64  `json:"blobCount"`
}

type storageAnalysisCategory struct {
	Code      storageanalysis.CategoryCode `json:"code"`
	Bytes     string                       `json:"bytes"`
	BlobCount int64                        `json:"blobCount"`
}

type storageAnalysisDetails struct {
	SaveStates        storageAnalysisSaveStates        `json:"saveStates"`
	CleanupCandidates storageAnalysisCleanupCandidates `json:"cleanupCandidates"`
}

type storageAnalysisSaveStates struct {
	ActiveCount              int64  `json:"activeCount"`
	DeletedCount             int64  `json:"deletedCount"`
	StateReferenceBytes      string `json:"stateReferenceBytes"`
	ScreenshotReferenceBytes string `json:"screenshotReferenceBytes"`
}

type storageAnalysisCleanupCandidates struct {
	BlobCount int64  `json:"blobCount"`
	Bytes     string `json:"bytes"`
}

func (server *Server) adminStorageAnalysis(writer http.ResponseWriter, request *http.Request) {
	if server.storageAnalysis == nil {
		server.databaseError(writer, request, errStorageAnalysisDatabaseMissing)
		return
	}
	snapshot, err := server.storageAnalysis.Analyze(request.Context())
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, storageAnalysisHTTPResponse(snapshot))
}

func storageAnalysisHTTPResponse(snapshot storageanalysis.Snapshot) storageAnalysisResponse {
	categories := make([]storageAnalysisCategory, len(snapshot.Categories))
	for index, category := range snapshot.Categories {
		categories[index] = storageAnalysisCategory{
			Code: category.Code, Bytes: decimalBytes(category.Bytes), BlobCount: category.BlobCount,
		}
	}
	return storageAnalysisResponse{
		Scope: snapshot.Scope, GeneratedAtMS: snapshot.GeneratedAtMS,
		Totals: storageAnalysisTotals{
			RegisteredBytes:   decimalBytes(snapshot.Totals.RegisteredBytes),
			ProtectedBytes:    decimalBytes(snapshot.Totals.ProtectedBytes),
			UnreferencedBytes: decimalBytes(snapshot.Totals.UnreferencedBytes),
			BlobCount:         snapshot.Totals.BlobCount,
		},
		Categories: categories,
		Details: storageAnalysisDetails{
			SaveStates: storageAnalysisSaveStates{
				ActiveCount:              snapshot.Details.SaveStates.ActiveCount,
				DeletedCount:             snapshot.Details.SaveStates.DeletedCount,
				StateReferenceBytes:      decimalBytes(snapshot.Details.SaveStates.StateReferenceBytes),
				ScreenshotReferenceBytes: decimalBytes(snapshot.Details.SaveStates.ScreenshotReferenceBytes),
			},
			CleanupCandidates: storageAnalysisCleanupCandidates{
				BlobCount: snapshot.Details.CleanupCandidates.BlobCount,
				Bytes:     decimalBytes(snapshot.Details.CleanupCandidates.Bytes),
			},
		},
		Excluded: snapshot.Excluded,
	}
}

func decimalBytes(value int64) string {
	return strconv.FormatInt(value, 10)
}

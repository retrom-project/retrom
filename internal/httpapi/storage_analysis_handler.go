package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"retrom/internal/authn"
	"retrom/internal/service/storageanalysis"
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
	RegisteredBytes    string `json:"registeredBytes"`
	RetainedBytes      string `json:"retainedBytes"`
	PendingDeleteBytes string `json:"pendingDeleteBytes"`
	FileCount          int64  `json:"fileCount"`
}

type storageAnalysisCategory struct {
	Code      storageanalysis.CategoryCode `json:"code"`
	Bytes     string                       `json:"bytes"`
	FileCount int64                        `json:"fileCount"`
}

type storageAnalysisDetails struct {
	SaveStates        storageAnalysisSaveStates        `json:"saveStates"`
	CleanupCandidates storageAnalysisCleanupCandidates `json:"cleanupCandidates"`
}

type storageAnalysisSaveStates struct {
	ActiveCount     int64  `json:"activeCount"`
	DeletedCount    int64  `json:"deletedCount"`
	StateBytes      string `json:"stateBytes"`
	ScreenshotBytes string `json:"screenshotBytes"`
}

type storageAnalysisCleanupCandidates struct {
	FileCount int64  `json:"fileCount"`
	Bytes     string `json:"bytes"`
}

type storageCleanupResponse struct {
	ScheduledFileCount int64  `json:"scheduledFileCount"`
	ScheduledBytes     string `json:"scheduledBytes"`
	AcceptedAtMS       int64  `json:"acceptedAtMs"`
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

func (server *Server) adminStorageCleanup(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	result, err := server.cleanupJobs.ScheduleImmediateDeletion(request.Context(), principal.UserID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusAccepted, storageCleanupResponse{
		ScheduledFileCount: result.FileCount,
		ScheduledBytes:     decimalBytes(result.Bytes),
		AcceptedAtMS:       result.AcceptedAtMS,
	})
}

func storageAnalysisHTTPResponse(snapshot storageanalysis.Snapshot) storageAnalysisResponse {
	categories := make([]storageAnalysisCategory, len(snapshot.Categories))
	for index, category := range snapshot.Categories {
		categories[index] = storageAnalysisCategory{
			Code: category.Code, Bytes: decimalBytes(category.Bytes), FileCount: category.FileCount,
		}
	}
	return storageAnalysisResponse{
		Scope: snapshot.Scope, GeneratedAtMS: snapshot.GeneratedAtMS,
		Totals: storageAnalysisTotals{
			RegisteredBytes:    decimalBytes(snapshot.Totals.RegisteredBytes),
			RetainedBytes:      decimalBytes(snapshot.Totals.RetainedBytes),
			PendingDeleteBytes: decimalBytes(snapshot.Totals.PendingDeleteBytes),
			FileCount:          snapshot.Totals.FileCount,
		},
		Categories: categories,
		Details: storageAnalysisDetails{
			SaveStates: storageAnalysisSaveStates{
				ActiveCount:     snapshot.Details.SaveStates.ActiveCount,
				DeletedCount:    snapshot.Details.SaveStates.DeletedCount,
				StateBytes:      decimalBytes(snapshot.Details.SaveStates.StateBytes),
				ScreenshotBytes: decimalBytes(snapshot.Details.SaveStates.ScreenshotBytes),
			},
			CleanupCandidates: storageAnalysisCleanupCandidates{
				FileCount: snapshot.Details.CleanupCandidates.FileCount,
				Bytes:     decimalBytes(snapshot.Details.CleanupCandidates.Bytes),
			},
		},
		Excluded: snapshot.Excluded,
	}
}

func decimalBytes(value int64) string {
	return strconv.FormatInt(value, 10)
}

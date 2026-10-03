package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"retrom/internal/service/importdiscard"
	"retrom/internal/service/libraryimport"
	"retrom/internal/service/sourceimport"
)

type importListView struct {
	libraryimport.ImportListItem
	Discard importdiscard.Status `json:"discard"`
}

type importDetailView struct {
	libraryimport.ImportDetail
	Discard importdiscard.Status `json:"discard"`
}

type sourceSummaryView struct {
	sourceimport.Summary
	Discard importdiscard.Status `json:"discard"`
}

func (server *Server) importListViews(ctx context.Context, items []importListItem) ([]importListView, error) {
	ids := make([]string, len(items))
	for index, item := range items {
		ids[index] = item.ID
	}
	statuses, err := server.importDeps.Discards.GetMany(ctx, "IMPORT", ids)
	if err != nil {
		return nil, fmt.Errorf("read import list discard state: %w", err)
	}
	result := make([]importListView, len(items))
	for index, item := range items {
		result[index] = importListView{item, statuses[item.ID]}
	}
	return result, nil
}

func (server *Server) sourceListViews(ctx context.Context, items []sourceimport.Summary) ([]sourceSummaryView, error) {
	ids := make([]string, len(items))
	for index, item := range items {
		ids[index] = item.ID
	}
	statuses, err := server.importDeps.Discards.GetMany(ctx, "SOURCE", ids)
	if err != nil {
		return nil, fmt.Errorf("read source list discard state: %w", err)
	}
	result := make([]sourceSummaryView, len(items))
	for index, item := range items {
		result[index] = sourceSummaryView{item, statuses[item.ID]}
	}
	return result, nil
}

func (server *Server) sourceSummaryWriter(ctx context.Context, request *http.Request) func(
	http.ResponseWriter, int, sourceimport.Summary,
) {
	return func(writer http.ResponseWriter, status int, summary sourceimport.Summary) {
		discard, err := server.importDeps.Discards.Get(ctx, "SOURCE", summary.ID)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, summary.Version))
		writeJSON(writer, status, sourceSummaryView{summary, discard})
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"retrom/internal/service/gameassets"
	"retrom/internal/service/idempotency"
	"retrom/internal/service/libraryimport"
	"retrom/internal/service/sourceimport"
)

type commandResponseSpec struct {
	status   int
	location string
}

// Every generic idempotent OpenAPI operation has an explicit response contract.
// Domain-managed operations retain their own transactional receipts.
var commandResponses = map[string]commandResponseSpec{
	"postAdminTag":                           {201, "/api/v1/admin/tags/"},
	"postAdminTagDefaults":                   {200, ""},
	"patchAdminTag":                          {200, ""},
	"deleteAdminTag":                         {204, ""},
	"putAdminGameTags":                       {200, ""},
	"postAdminGameAsset":                     {201, ""},
	"deleteAdminGameAsset":                   {204, ""},
	"postAdminGameScrapeCandidates":          {202, ""},
	"postAdminGameScrapeCandidateApply":      {200, ""},
	"postAdminGameMovePreview":               {200, ""},
	"postAdminGameMove":                      {200, ""},
	"postAdminPlatformDefaultCore":           {200, ""},
	"postAdminImportBatchDiscard":            {202, ""},
	"postAdminUpload":                        {201, ""},
	"postAdminUploadComplete":                {202, ""},
	"postAdminImport":                        {202, "/api/v1/admin/imports/"},
	"postAdminImportCancel":                  {200, ""},
	"postAdminImportReconfigure":             {202, "/api/v1/admin/imports/"},
	"postAdminImportItemRetry":               {202, ""},
	"postAdminJobCancel":                     {200, ""},
	"postAdminJobRetry":                      {202, ""},
	"postAdminReviewDeduplicate":             {200, ""},
	"postAdminReviewBulkApproval":            {202, ""},
	"postAdminReviewScrapeCandidates":        {202, ""},
	"postAdminReviewAsset":                   {201, ""},
	"postAdminReviewArcadeParentAttachment":  {202, "/api/v1/admin/jobs/"},
	"postAdminReviewMultiDiscAttachment":     {202, "/api/v1/admin/jobs/"},
	"postAdminReviewPreview":                 {201, ""},
	"postAdminReviewApprove":                 {201, ""},
	"postAdminReviewDiscard":                 {200, ""},
	"postAdminServerImport":                  {202, "/api/v1/admin/server-imports/"},
	"postAdminServerImportCancel":            {200, ""},
	"postAdminServerImportRetry":             {202, ""},
	"postAdminBIOSInstallation":              {201, ""},
	"postAdminSourceImport":                  {202, "/api/v1/admin/source-imports/"},
	"deleteAdminSourceImport":                {204, ""},
	"putAdminSourceImportCollectionMappings": {200, ""},
	"postAdminSourceImportStart":             {202, ""},
	"postAdminSourceImportRetry":             {202, ""},
}

func (server *Server) commandEncoder(operation string) idempotency.Encoder {
	return func(ctx context.Context, value any) (idempotency.Receipt, error) {
		result, ok := value.(idempotency.Result)
		if !ok {
			return idempotency.Receipt{}, idempotency.ErrInvalidReceipt
		}
		spec := commandResponses[operation]
		status := spec.status
		if result.Accepted {
			status = http.StatusAccepted
		}
		if result.Created {
			status = http.StatusCreated
		}
		headers := map[string]string{}
		if result.Version > 0 {
			headers["ETag"] = fmt.Sprintf(`"v%d"`, result.Version)
		}
		if spec.location != "" && result.ResourceID != "" {
			headers["Location"] = spec.location + result.ResourceID
		}
		body := make([]byte, 0)
		if status != http.StatusNoContent {
			projected, err := server.projectCommandBody(ctx, result.Value)
			if err != nil {
				return idempotency.Receipt{}, err
			}
			body, err = json.Marshal(projected)
			if err != nil {
				return idempotency.Receipt{}, fmt.Errorf("encode command body: %w", err)
			}
			body = append(body, '\n')
			headers["Content-Type"] = "application/json; charset=utf-8"
		}
		encoded, err := json.Marshal(headers)
		if err != nil {
			return idempotency.Receipt{}, fmt.Errorf("encode command headers: %w", err)
		}
		return idempotency.Receipt{HTTPStatus: status, HeadersJSON: string(encoded), Body: body}, nil
	}
}

func (server *Server) projectCommandBody(ctx context.Context, value any) (any, error) {
	switch result := value.(type) {
	case sourceimport.Summary:
		discard, err := server.importDeps.Discards.Get(ctx, "SOURCE", result.ID)
		if err != nil {
			return nil, fmt.Errorf("freeze Source discard projection: %w", err)
		}
		return sourceSummaryView{result, discard}, nil
	case gameassets.CreateResult:
		return map[string]any{
			"assetId": result.AssetID, "gameId": result.GameID, "kind": result.Kind,
			"ordinal": result.Ordinal, "widthPx": result.WidthPX, "heightPx": result.HeightPX,
			"mediaType": result.MediaType, "version": result.Version, "createdAtMs": result.CreatedAtMS,
		}, nil
	case libraryimport.ReviewAssetResult:
		return struct {
			libraryimport.ReviewAssetResult
			URL string `json:"url"`
		}{result, "/api/v1/admin/review-assets/" + result.AssetID}, nil
	default:
		return value, nil
	}
}

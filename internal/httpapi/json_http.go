package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"retrom/internal/service/mediaaccess"

	"retrom/internal/dbexec"

	"retrom/internal/cleanup"
)

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any, limit int64) error {
	if mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		return errJSONContentType
	}
	contents, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, limit))
	if err != nil {
		return fmt.Errorf("httpapi/server: %w", err)
	}
	if !utf8.Valid(contents) {
		return errJSONUTF8
	}
	if err := validateJSONLexical(contents, 64); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("httpapi/server: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errJSONTrailing
		}
		return fmt.Errorf("httpapi/server: %w", err)
	}
	return nil
}

type lexicalJSONParser struct {
	decoder  *json.Decoder
	maxDepth int
}

func validateJSONLexical(contents []byte, maxDepth int) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	parser := lexicalJSONParser{decoder: decoder, maxDepth: maxDepth}
	if err := parser.parseValue(1); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errJSONTrailing
		}
		return fmt.Errorf("httpapi/server: %w", err)
	}
	return nil
}

func (parser lexicalJSONParser) parseValue(depth int) error {
	if depth > parser.maxDepth {
		return errJSONNesting
	}
	token, err := parser.decoder.Token()
	if err != nil {
		return fmt.Errorf("httpapi/server: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		if err := parser.parseObject(depth); err != nil {
			return err
		}
	case '[':
		if err := parser.parseArray(depth); err != nil {
			return err
		}
	default:
		return errJSONDelimiter
	}
	return parser.consumeClosing(delimiter)
}

func (parser lexicalJSONParser) parseObject(depth int) error {
	keys := map[string]struct{}{}
	for parser.decoder.More() {
		keyToken, err := parser.decoder.Token()
		if err != nil {
			return fmt.Errorf("httpapi/server: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return errJSONObjectKey
		}
		if _, exists := keys[key]; exists {
			return errJSONDuplicateKey
		}
		keys[key] = struct{}{}
		if err := parser.parseValue(depth + 1); err != nil {
			return err
		}
	}
	return nil
}

func (parser lexicalJSONParser) parseArray(depth int) error {
	for parser.decoder.More() {
		if err := parser.parseValue(depth + 1); err != nil {
			return err
		}
	}
	return nil
}

func (parser lexicalJSONParser) consumeClosing(opening json.Delim) error {
	closing, err := parser.decoder.Token()
	if err != nil {
		return fmt.Errorf("httpapi/server: %w", err)
	}
	expected := map[json.Delim]json.Delim{'{': '}', '[': ']'}[opening]
	if closing != expected {
		return errJSONClosingDelimiter
	}
	return nil
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func gameCoverURL(assetID sql.NullString) any {
	if assetID.Valid {
		return "/content/assets/" + assetID.String
	}
	return nil
}

func saveStateScreenshotURL(saveStateID string) string {
	return "/content/save-states/" + saveStateID + "/screenshot"
}

func (server *Server) reviewCandidateAsset(writer http.ResponseWriter, request *http.Request) {
	asset, err := server.mediaAccess.Review(request.Context(), request.PathValue("assetId"), request.URL.Query().Get("kind"))
	switch {
	case errors.Is(err, mediaaccess.ErrKind):
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "审核来源媒体类型无效", map[string]any{})
		return
	case errors.Is(err, mediaaccess.ErrNotFound):
		writeError(writer, request, http.StatusNotFound, "REVIEW_ASSET_NOT_FOUND", "候选媒体不存在", map[string]any{})
		return
	case err != nil:
		server.databaseError(writer, request, err)
		return
	}
	server.serveBlob(writer, request, asset.Digest, asset.MediaType, true)
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) diagnostics(writer http.ResponseWriter, request *http.Request) {
	transaction, err := server.database.BeginTx(request.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer dbexec.Rollback(transaction)
	var schemaVersion int64
	if err := transaction.QueryRowContext(request.Context(), `
SELECT COALESCE(MAX(version),
0)
FROM schema_migrations
`).Scan(&schemaVersion); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var publishedGames, deletedGames, activeSaves, deletedSaves, blobCount int64
	var queuedJobs, runningJobs, cancelRequestedJobs, succeededJobs, failedJobs, cancelledJobs int64
	var pendingDATs, parsingDATs, readyDATs, failedDATs, cancelledDATs int64
	err = transaction.QueryRowContext(request.Context(), `
SELECT
(SELECT count(*)
FROM games
WHERE status='PUBLISHED'),
(SELECT count(*)
FROM games
WHERE status='DELETED'),
(SELECT count(*)
FROM save_states
WHERE deleted_at_ms IS NULL),
(SELECT count(*)
FROM save_states
WHERE deleted_at_ms IS NOT NULL),
(SELECT count(*)
FROM blobs),
(SELECT count(*)
FROM jobs
WHERE state='QUEUED'),
(SELECT count(*)
FROM jobs
WHERE state='RUNNING'),
(SELECT count(*)
FROM jobs
WHERE state='CANCEL_REQUESTED'),
(SELECT count(*)
FROM jobs
WHERE state='SUCCEEDED'),
(SELECT count(*)
FROM jobs
WHERE state='FAILED'),
(SELECT count(*)
FROM jobs
WHERE state='CANCELLED'),
(SELECT count(*)
FROM dat_versions
WHERE parse_status='PENDING'),
(SELECT count(*)
FROM dat_versions
WHERE parse_status='PARSING'),
(SELECT count(*)
FROM dat_versions
WHERE parse_status='READY'),
(SELECT count(*)
FROM dat_versions
WHERE parse_status='FAILED'),
(SELECT count(*)
FROM dat_versions
WHERE parse_status='CANCELLED')
`).Scan(
		&publishedGames, &deletedGames, &activeSaves, &deletedSaves, &blobCount,
		&queuedJobs, &runningJobs, &cancelRequestedJobs, &succeededJobs, &failedJobs, &cancelledJobs,
		&pendingDATs, &parsingDATs, &readyDATs, &failedDATs, &cancelledDATs,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	providerRows, err := transaction.QueryContext(request.Context(), `
SELECT provider_id,provider_version,bundle_sha256,source
FROM runtime_providers ORDER BY provider_id
`)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close provider rows", providerRows.Close()) }()
	runtimeProviders := make([]map[string]any, 0, 2)
	for providerRows.Next() {
		var providerID, providerVersion, bundleSHA256, source string
		if err := providerRows.Scan(&providerID, &providerVersion, &bundleSHA256, &source); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		runtimeProviders = append(runtimeProviders, map[string]any{
			"providerId": providerID, "providerVersion": providerVersion,
			"bundleSha256": bundleSHA256, "source": source,
		})
	}
	providerErr := providerRows.Err()
	if providerErr != nil {
		server.databaseError(writer, request, providerErr)
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Content-Disposition", `attachment; filename="retrom-diagnostics.json"`)
	writeJSON(writer, http.StatusOK, map[string]any{
		"schemaVersion": 2, "generatedAtMs": server.now().UnixMilli(), "databaseSchemaVersion": schemaVersion,
		"runtimeProviders": runtimeProviders,
		"counts": map[string]any{
			"games":      map[string]any{"published": publishedGames, "deleted": deletedGames},
			"saveStates": map[string]any{"active": activeSaves, "deleted": deletedSaves},
			"blobs":      blobCount,
			"jobs": map[string]any{
				"queued":          queuedJobs,
				"running":         runningJobs,
				"cancelRequested": cancelRequestedJobs,
				"succeeded":       succeededJobs,
				"failed":          failedJobs,
				"cancelled":       cancelledJobs,
			},
			"datVersions": map[string]any{
				"pending":   pendingDATs,
				"parsing":   parsingDATs,
				"ready":     readyDATs,
				"failed":    failedDATs,
				"cancelled": cancelledDATs,
			},
		},
	})
}

func (server *Server) notFound(writer http.ResponseWriter, request *http.Request) {
	writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "资源不存在", map[string]any{})
}

func (server *Server) databaseError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	slog.ErrorContext(
		request.Context(),
		"database operation failed",
		"requestId",
		request.Context().Value(requestIDKey),
		"error",
		err,
	)
	writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "数据库操作失败", map[string]any{})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	if writer.Header().Get("Cache-Control") == "" {
		writer.Header().Set("Cache-Control", "private, no-store")
	}
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(
	writer http.ResponseWriter,
	request *http.Request,
	status int,
	code, message string,
	details map[string]any,
) {
	requestID, _ := request.Context().Value(requestIDKey).(string)
	writeJSON(
		writer,
		status,
		map[string]any{
			"error": map[string]any{"code": code, "message": message, "details": details, "requestId": requestID},
		},
	)
}

// ParseETag accepts only the exact strong resource-version representation.
func ParseETag(value string) (int64, error) {
	if len(value) < 4 || !strings.HasPrefix(value, `"v`) || !strings.HasSuffix(value, `"`) {
		return 0, errInvalidETag
	}
	version, err := strconv.ParseInt(value[2:len(value)-1], 10, 64)
	if err != nil || version < 1 || fmt.Sprintf(`"v%d"`, version) != value {
		return 0, errInvalidETag
	}
	return version, nil
}

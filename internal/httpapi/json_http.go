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
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

func reviewAssetURL(assetID sql.NullString) any {
	if assetID.Valid {
		return "/api/v1/admin/review-assets/" + assetID.String
	}
	return nil
}

func saveStateScreenshotURL(saveStateID string) string {
	return "/content/save-states/" + saveStateID + "/screenshot"
}

func nullableInt64(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func (server *Server) reviewCandidateAsset(writer http.ResponseWriter, request *http.Request) {
	var digest, mediaType string
	err := server.database.QueryRowContext(request.Context(), `
SELECT digest,media_type FROM (
  SELECT b.sha256 AS digest,a.media_type AS media_type
  FROM scrape_candidate_assets a
  JOIN blobs b ON b.id=a.blob_id
  JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
  JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
  LEFT JOIN import_items i ON i.id=r.import_item_id
  LEFT JOIN games g ON g.id=r.game_id
  WHERE a.id=? AND a.status='READY'
  AND (i.state='REVIEW_PENDING' OR g.status='PUBLISHED' OR EXISTS (
    SELECT 1 FROM review_events e WHERE e.import_item_id=i.id AND e.event_type IN ('APPROVED','DISCARDED')
  ))
  UNION ALL
  SELECT b.sha256 AS digest,a.media_type AS media_type
  FROM review_uploaded_assets a
  JOIN blobs b ON b.id=a.blob_id
  JOIN import_items i ON i.id=a.import_item_id
  WHERE a.id=?
  AND (i.state='REVIEW_PENDING' OR EXISTS (
    SELECT 1 FROM review_events e WHERE e.import_item_id=i.id AND e.event_type IN ('APPROVED','DISCARDED')
  ))
  UNION ALL
  SELECT b.sha256 AS digest,screenshot.media_type AS media_type
  FROM review_runtime_screenshots screenshot
  JOIN blobs b ON b.id=screenshot.blob_id
  JOIN import_items i ON i.id=screenshot.import_item_id
  WHERE screenshot.id=?
  AND (i.state='REVIEW_PENDING' OR EXISTS (
    SELECT 1 FROM review_events e WHERE e.import_item_id=i.id AND e.event_type IN ('APPROVED','DISCARDED')
  ))
  UNION ALL
  SELECT b.sha256 AS digest,'image/png' AS media_type
  FROM rpgmaker_runtime_validations validation
  JOIN blobs b ON b.id=validation.evidence_screenshot_blob_id
  JOIN import_items i ON i.id=validation.import_item_id
  WHERE validation.id=?
  AND (i.state='REVIEW_PENDING' OR EXISTS (
    SELECT 1 FROM review_events e WHERE e.import_item_id=i.id AND e.event_type IN ('APPROVED','DISCARDED')
  ))
) LIMIT 1
`, request.PathValue("assetId"), request.PathValue("assetId"), request.PathValue("assetId"),
		request.PathValue("assetId")).Scan(&digest, &mediaType)
	if errors.Is(err, sql.ErrNoRows) {
		kind := request.URL.Query().Get("kind")
		if kind == "" {
			kind = "COVER"
		}
		if kind != "COVER" && kind != "VIDEO" {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "审核来源媒体类型无效", map[string]any{})
			return
		}
		err = server.database.QueryRowContext(request.Context(), `
SELECT min(candidate.digest),min(candidate.media_type) FROM (
 SELECT blob.sha256 AS digest,asset.media_type
 FROM pegasus_import_item_assets asset
 JOIN blobs blob ON blob.id=asset.blob_id
 JOIN pegasus_import_items source ON source.id=asset.item_id
 JOIN import_items item ON item.id=source.library_import_item_id
 WHERE source.id=? AND asset.kind=? AND asset.state='COPIED'
 AND (item.state='REVIEW_PENDING' OR EXISTS(
  SELECT 1 FROM review_events event
  WHERE event.import_item_id=item.id AND event.event_type IN ('APPROVED','DISCARDED')
 ))
 UNION ALL
 SELECT blob.sha256 AS digest,asset.media_type
 FROM emulationstation_import_item_assets asset
 JOIN blobs blob ON blob.id=asset.blob_id
 JOIN emulationstation_import_items source ON source.id=asset.item_id
 JOIN import_items item ON item.id=source.library_import_item_id
 WHERE source.id=? AND asset.kind=? AND asset.state='COPIED'
 AND (item.state='REVIEW_PENDING' OR EXISTS(
  SELECT 1 FROM review_events event
  WHERE event.import_item_id=item.id AND event.event_type IN ('APPROVED','DISCARDED')
 ))
) candidate HAVING count(*)=1
`, request.PathValue("assetId"), kind, request.PathValue("assetId"), kind).Scan(&digest, &mediaType)
	}
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "REVIEW_ASSET_NOT_FOUND", "候选媒体不存在", map[string]any{})
		return
	}
	server.serveBlob(writer, request, digest, mediaType, true)
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) diagnostics(writer http.ResponseWriter, request *http.Request) {
	transaction, err := server.database.BeginTx(request.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
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
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	versions := make([]string, 0, len(server.dependencies.Versions))
	for version := range server.dependencies.Versions {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(left, right int) bool { return semverLess(versions[left], versions[right]) })
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Content-Disposition", `attachment; filename="retrom-diagnostics.json"`)
	writeJSON(writer, http.StatusOK, map[string]any{
		"schemaVersion": 1, "generatedAtMs": server.now().UnixMilli(), "databaseSchemaVersion": schemaVersion,
		"dependencies": map[string]any{
			"configuredEmulatorjsVersions": versions,
			"activeEmulatorjsVersion":      server.config.ActiveEJSVersion,
		},
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

func semverLess(left, right string) bool {
	leftParts, rightParts := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < 3; index++ {
		var leftPart, rightPart int64
		if index < len(leftParts) {
			leftPart, _ = strconv.ParseInt(strings.SplitN(leftParts[index], "-", 2)[0], 10, 64)
		}
		if index < len(rightParts) {
			rightPart, _ = strconv.ParseInt(strings.SplitN(rightParts[index], "-", 2)[0], 10, 64)
		}
		if leftPart != rightPart {
			return leftPart < rightPart
		}
	}
	return left < right
}

func (server *Server) runtimeFile(writer http.ResponseWriter, request *http.Request) {
	versionName := request.PathValue("configuredVersion")
	runtimePath := request.PathValue("runtimePath")
	version := server.dependencies.Versions[versionName]
	if version == nil {
		server.notFound(writer, request)
		return
	}
	declaration, ok := version.Allowlist[runtimePath]
	if !ok {
		server.notFound(writer, request)
		return
	}
	path := filepath.Join(version.RuntimeRoot, filepath.FromSlash(runtimePath))
	file, err := os.Open(path)
	if err != nil {
		server.notFound(writer, request)
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != declaration.SizeBytes {
		writeError(writer, request, http.StatusServiceUnavailable, "DEPENDENCY_INVALID", "运行时依赖不可用", map[string]any{})
		return
	}
	mediaType := mime.TypeByExtension(filepath.Ext(runtimePath))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	writer.Header().Set("ETag", `"sha256-`+declaration.SHA256+`"`)
	http.ServeContent(writer, request, filepath.Base(runtimePath), time.Unix(0, 0), file)
}

func (server *Server) rpgMakerRuntimeFile(writer http.ResponseWriter, request *http.Request) {
	runtimePath, declaration, ok := server.dependencies.RPGMakerFile(
		request.PathValue("runtimeVersion"), request.PathValue("runtimePath"),
	)
	if !ok {
		server.notFound(writer, request)
		return
	}
	file, err := os.Open(runtimePath)
	if err != nil {
		server.notFound(writer, request)
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != declaration.SizeBytes {
		writeError(
			writer, request, http.StatusServiceUnavailable, "DEPENDENCY_INVALID",
			"RPG Maker 运行时依赖不可用", map[string]any{},
		)
		return
	}
	mediaType := map[string]string{
		".js": "application/javascript; charset=utf-8", ".wasm": "application/wasm", ".rb": "text/plain; charset=utf-8",
	}[strings.ToLower(filepath.Ext(request.PathValue("runtimePath")))]
	if mediaType == "" {
		server.notFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	writer.Header().Set("ETag", `"sha256-`+declaration.SHA256+`"`)
	http.ServeContent(writer, request, filepath.Base(runtimePath), time.Unix(0, 0), file)
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

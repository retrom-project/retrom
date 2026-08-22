package saves

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"

	// Register the JPEG and PNG decoders used for save-state screenshots.
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	retromruntime "retrom/internal/runtime"
)

const (
	maxStateBytes      = int64(64 << 20)
	maxScreenshotBytes = int64(10 << 20)
	maxPixels          = int64(40_000_000)
)

var (
	ErrCredential            = errors.New("LAUNCH_CREDENTIAL_INVALID")
	ErrInvalid               = errors.New("SAVE_INVALID")
	ErrTooLarge              = errors.New("SAVE_TOO_LARGE")
	ErrIdempotencyReused     = errors.New("IDEMPOTENCY_KEY_REUSED")
	ErrSequenceGap           = errors.New("SAVE_SEQUENCE_GAP")
	ErrSequenceReused        = errors.New("SAVE_SEQUENCE_REUSED")
	ErrPersistentConflict    = errors.New("PERSISTENT_SAVE_CONFLICT")
	ErrPersistentUnsupported = errors.New("PERSISTENT_SAVE_UNSUPPORTED")
)

type Service struct {
	database    *sql.DB
	blobs       *blobstore.Store
	credentials *retromruntime.Credentials
	now         func() time.Time
}

func New(
	database *sql.DB,
	blobs *blobstore.Store,
	credentials *retromruntime.Credentials,
	now func() time.Time,
) *Service {
	return &Service{database: database, blobs: blobs, credentials: credentials, now: now}
}

type ManualResult struct {
	SaveStateID      string `json:"saveStateId"`
	Name             string `json:"name"`
	DiscIndex        *int   `json:"discIndex"`
	Version          int64  `json:"version"`
	CreatedAtMS      int64  `json:"createdAtMs"`
	ActiveDurationMS int64  `json:"activeDurationMs"`
}

type manualMetadata struct {
	Name      string `json:"name"`
	DiscIndex *int   `json:"discIndex"`
}

type launchSnapshot struct {
	principalID, profileID, gameID, variantRevisionID, artifactID string
	persistentSaveMode, persistentSaveKind, runtimeCoreID         string
	datVersionID, dosEntry                                        sql.NullString
	credentialHash                                                []byte
	state                                                         string
	hardExpiresAtMS                                               int64
	contentFormat                                                 string
	discCount, initialDiscIndex                                   int
}

func (service *Service) launch(ctx context.Context, launchID, capability string) (launchSnapshot, error) {
	var result launchSnapshot
	var compatibilityJSON string
	err := service.database.QueryRowContext(ctx, `
SELECT COALESCE(u.id,l.profile_id),
l.profile_id,
l.game_id,
l.game_variant_revision_id,
l.core_artifact_id,
r.dat_version_id,
l.dos_entry_path,
l.credential_sha256,
l.state,
l.hard_expires_at_ms,
a.compatibility_config_json
,
content.format_version,
(SELECT count(*) FROM launch_external_files external
 WHERE external.launch_session_id=l.id AND external.kind='DISC'),
l.initial_disc_index
FROM launch_sessions l
JOIN game_variant_revisions r ON r.id=l.game_variant_revision_id
JOIN core_artifacts a ON a.id=l.core_artifact_id
JOIN launch_content_files content ON content.launch_session_id=l.id
LEFT JOIN users u ON u.profile_id=l.profile_id
WHERE l.id=?
`, launchID).
		Scan(
			&result.principalID, &result.profileID, &result.gameID, &result.variantRevisionID, &result.artifactID,
			&result.datVersionID, &result.dosEntry, &result.credentialHash, &result.state, &result.hardExpiresAtMS,
			&compatibilityJSON,
			&result.contentFormat, &result.discCount, &result.initialDiscIndex,
		)
	if err != nil || !retromruntime.MatchesCapability(capability, result.credentialHash) ||
		result.state != "ACTIVE" || result.hardExpiresAtMS <= service.now().UnixMilli() {
		return launchSnapshot{}, ErrCredential
	}
	if err := applySaveCompatibility(&result, compatibilityJSON); err != nil {
		return launchSnapshot{}, ErrCredential
	}
	if !validLaunchDiscShape(result) {
		return launchSnapshot{}, ErrCredential
	}
	return result, nil
}

func applySaveCompatibility(result *launchSnapshot, raw string) error {
	var compatibility struct {
		SchemaVersion      int     `json:"schemaVersion"`
		RuntimeCoreID      string  `json:"runtimeCoreId"`
		PersistentSaveMode string  `json:"persistentSaveMode"`
		PersistentSaveKind *string `json:"persistentSaveKind"`
	}
	if err := json.Unmarshal([]byte(raw), &compatibility); err != nil ||
		compatibility.SchemaVersion != 2 && compatibility.SchemaVersion != 3 && compatibility.SchemaVersion != 4 {
		return ErrCredential
	}
	result.persistentSaveMode = compatibility.PersistentSaveMode
	result.runtimeCoreID = compatibility.RuntimeCoreID
	if compatibility.PersistentSaveKind != nil {
		result.persistentSaveKind = *compatibility.PersistentSaveKind
	}
	return nil
}

func validLaunchDiscShape(result launchSnapshot) bool {
	if result.contentFormat != "RETROM_MULTIDISC_M3U_V1" {
		return result.discCount == 0 && result.initialDiscIndex == 0
	}
	return result.discCount >= 2 && result.initialDiscIndex >= 0 && result.initialDiscIndex < result.discCount
}

func validName(name string) bool {
	if name != strings.TrimSpace(name) || !utf8.ValidString(name) {
		return false
	}
	count := 0
	for _, value := range name {
		if unicode.IsControl(value) {
			return false
		}
		count++
	}
	return count >= 1 && count <= 120
}

type parsedManual struct {
	metadata            manualMetadata
	state, screenshot   blobstore.Metadata
	screenshotMediaType string
}

func (service *Service) readBounded(source io.Reader, maximum int64) (blobstore.Metadata, error) {
	metadata, err := service.blobs.Put(io.LimitReader(source, maximum+1))
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("saves/service: %w", err)
	}
	if metadata.Size > maximum {
		return blobstore.Metadata{}, ErrTooLarge
	}
	if metadata.Size == 0 {
		return blobstore.Metadata{}, ErrInvalid
	}
	return metadata, nil
}

func (service *Service) parseManual(request *http.Request) (parsedManual, error) {
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		return parsedManual{}, ErrInvalid
	}
	reader := multipart.NewReader(request.Body, parameters["boundary"])
	var result parsedManual
	seen := map[string]bool{}
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return parsedManual{}, ErrInvalid
		}
		name := part.FormName()
		if seen[name] || name != "metadata" && name != "state" && name != "screenshot" {
			cleanup.Error("close", part.Close())
			return parsedManual{}, ErrInvalid
		}
		seen[name] = true
		err = service.parseManualPart(part, name, &result)
		cleanup.Error("close", part.Close())
		if err != nil {
			return parsedManual{}, err
		}
	}
	if !seen["metadata"] || !seen["state"] || !seen["screenshot"] {
		return parsedManual{}, ErrInvalid
	}
	return result, nil
}

func (service *Service) parseManualPart(part *multipart.Part, name string, result *parsedManual) error {
	switch name {
	case "metadata":
		return parseManualMetadata(part, &result.metadata)
	case "state":
		metadata, err := service.readBounded(part, maxStateBytes)
		result.state = metadata
		return err
	case "screenshot":
		metadata, err := service.readBounded(part, maxScreenshotBytes)
		result.screenshot = metadata
		if err != nil {
			return err
		}
		result.screenshotMediaType, err = validateScreenshot(metadata.Path)
		return err
	default:
		return ErrInvalid
	}
}

func parseManualMetadata(part *multipart.Part, metadata *manualMetadata) error {
	if value := part.Header.Get("Content-Type"); value != "" && value != "application/json" {
		return ErrInvalid
	}
	decoder := jsonDecoder(io.LimitReader(part, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(metadata); err != nil || !validName(metadata.Name) {
		return ErrInvalid
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return ErrInvalid
	}
	return nil
}

// jsonDecoder is a seam kept narrow so multipart body handling never invokes ParseMultipartForm.
var jsonDecoder = func(reader io.Reader) interface {
	Decode(any) error
	DisallowUnknownFields()
} {
	return newStrictDecoder(reader)
}

func validateScreenshot(path string) (string, error) {
	file, err := osOpen(path)
	if err != nil {
		return "", ErrInvalid
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	header := make([]byte, 512)
	read, _ := io.ReadFull(file, header)
	mediaType := http.DetectContentType(header[:read])
	if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/webp" {
		return "", ErrInvalid
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", ErrInvalid
	}
	configuration, _, err := image.DecodeConfig(file)
	if err != nil || configuration.Width <= 0 || configuration.Height <= 0 ||
		int64(configuration.Width)*int64(configuration.Height) > maxPixels {
		// The standard library does not decode WebP. It is still accepted only when
		// its RIFF dimensions can be verified by the compact parser below.
		if mediaType != "image/webp" {
			return "", ErrInvalid
		}
		if ok := validWebPDimensions(path); !ok {
			return "", ErrInvalid
		}
	}
	return mediaType, nil
}

// These seams permit deterministic read-failure tests without changing the CAS contract.
var osOpen = os.Open

func validManualDiscIndex(launch launchSnapshot, discIndex *int) bool {
	if launch.contentFormat != "RETROM_MULTIDISC_M3U_V1" {
		return discIndex == nil
	}
	return discIndex != nil && *discIndex >= 0 && *discIndex < launch.discCount
}

func (service *Service) replayManualSave(
	ctx context.Context,
	transaction *sql.Tx,
	principalID, idempotencyKey, requestDigest string,
) (ManualResult, bool, error) {
	var storedDigest string
	var storedBody []byte
	err := transaction.QueryRowContext(ctx, `
SELECT request_digest,
response_body
FROM idempotency_records
WHERE operation_id='postRuntimeSaveState'
AND key=?
AND principal_id=?
AND expires_at_ms>?
`, idempotencyKey, principalID, service.now().UnixMilli()).Scan(&storedDigest, &storedBody)
	if errors.Is(err, sql.ErrNoRows) {
		return ManualResult{}, false, nil
	}
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(storedDigest), []byte(requestDigest)) != 1 {
		return ManualResult{}, false, ErrSequenceReused
	}
	var previous ManualResult
	if err := json.Unmarshal(storedBody, &previous); err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	return previous, true, nil
}

func validWebPDimensions(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	data := make([]byte, 30)
	read, err := io.ReadFull(file, data)
	if err != nil || read < 30 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false
	}
	var width, height int64
	switch string(data[12:16]) {
	case "VP8X":
		width = int64(data[24]) | int64(data[25])<<8 | int64(data[26])<<16
		height = int64(data[27]) | int64(data[28])<<8 | int64(data[29])<<16
		width++
		height++
	default:
		return false
	}
	return width > 0 && height > 0 && width*height <= maxPixels
}

func (service *Service) CreateManual(
	ctx context.Context,
	launchID, capability, idempotencyKey string,
	request *http.Request,
) (ManualResult, bool, error) {
	launch, err := service.launch(ctx, launchID, capability)
	if err != nil {
		return ManualResult{}, false, err
	}
	parsed, err := service.parseManual(request)
	if err != nil {
		return ManualResult{}, false, err
	}
	if !validManualDiscIndex(launch, parsed.metadata.DiscIndex) {
		return ManualResult{}, false, ErrInvalid
	}
	metadataDigest, _ := json.Marshal(parsed.metadata)
	digest := sha256.Sum256(
		[]byte(string(metadataDigest) + "\x00" + parsed.state.SHA256 + "\x00" + parsed.screenshot.SHA256),
	)
	requestDigest := hex.EncodeToString(digest[:])
	return service.persistManualSave(ctx, launchID, idempotencyKey, requestDigest, launch, parsed)
}

func (service *Service) persistManualSave(
	ctx context.Context,
	launchID, idempotencyKey, requestDigest string,
	launch launchSnapshot,
	parsed parsedManual,
) (ManualResult, bool, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if previous, replayed, err := service.replayManualSave(
		ctx, transaction, launch.principalID, idempotencyKey, requestDigest,
	); err != nil || replayed {
		return previous, replayed, err
	}
	now := service.now().UnixMilli()
	stateID, err := blobstore.EnsureRecord(ctx, transaction, parsed.state, "application/octet-stream", now)
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	screenshotID, err := blobstore.EnsureRecord(ctx, transaction, parsed.screenshot, parsed.screenshotMediaType, now)
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	var activeDuration int64
	_ = transaction.QueryRowContext(ctx, `
SELECT active_duration_ms
FROM play_sessions
WHERE launch_session_id=?
`, launchID).
		Scan(&activeDuration)
	generated, err := uuid.NewV7()
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	result := ManualResult{
		SaveStateID:      generated.String(),
		Name:             parsed.metadata.Name,
		DiscIndex:        parsed.metadata.DiscIndex,
		Version:          1,
		CreatedAtMS:      now,
		ActiveDurationMS: activeDuration,
	}
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO save_states(id,
profile_id,
game_id,
game_variant_revision_id,
core_artifact_id,
dat_version_id,
dos_entry_path,
disc_index,
state_blob_id,
screenshot_blob_id,
source_launch_session_id,
name,
active_duration_ms,
version,
created_at_ms,
updated_at_ms)
VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
1,
?,
?)
`,
		result.SaveStateID,
		launch.profileID,
		launch.gameID,
		launch.variantRevisionID,
		launch.artifactID,
		nullable(
			launch.datVersionID,
		),
		nullable(launch.dosEntry),
		parsed.metadata.DiscIndex,
		stateID,
		screenshotID,
		launchID,
		result.Name,
		activeDuration,
		now,
		now,
	)
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	body, _ := json.Marshal(result)
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO idempotency_records(principal_id,
operation_id,
key,
request_digest,
http_status,
response_headers_json,
response_body,
created_at_ms,
expires_at_ms)
VALUES(?,
'postRuntimeSaveState',
?,
?,
201,
'{}',
?,
?,
?)
`,
		launch.principalID,
		idempotencyKey,
		requestDigest,
		body,
		now,
		now+int64(24*time.Hour/time.Millisecond),
	)
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	return result, false, nil
}

func nullable(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

type PersistentResult struct {
	PersistentSaveID string `json:"persistentSaveId"`
	RevisionID       string `json:"revisionId"`
	Sequence         int64  `json:"sequence"`
	CreatedAtMS      int64  `json:"createdAtMs"`
}

type persistentWrite struct {
	launchID, idempotencyKey, event, requestDigest string
	sequence, now                                  int64
	launch                                         launchSnapshot
	metadata                                       blobstore.Metadata
}

func (service *Service) GetPersistent(
	ctx context.Context,
	launchID, capability string,
) (blobstore.Metadata, bool, error) {
	launch, err := service.launch(ctx, launchID, capability)
	if err != nil {
		return blobstore.Metadata{}, false, err
	}
	if launch.persistentSaveMode == "NONE" {
		return blobstore.Metadata{}, false, ErrPersistentUnsupported
	}
	var digest, mediaType string
	var size int64
	err = service.database.QueryRowContext(ctx, `
SELECT b.sha256,
b.size_bytes,
b.media_type
FROM launch_sessions l
JOIN persistent_save_revisions r ON r.id=l.persistent_save_base_revision_id
JOIN blobs b ON b.id=r.blob_id
WHERE l.id=?
`, launchID).
		Scan(&digest, &size, &mediaType)
	if errors.Is(err, sql.ErrNoRows) {
		return blobstore.Metadata{}, false, nil
	}
	if err != nil {
		return blobstore.Metadata{}, false, fmt.Errorf("saves/service: %w", err)
	}
	if size > maxStateBytes {
		return blobstore.Metadata{}, false, ErrTooLarge
	}
	return blobstore.Metadata{SHA256: digest, Size: size, Path: service.blobs.Path(digest)}, true, nil
}

func parseRFC9530(value string) (string, error) {
	if !strings.HasPrefix(value, "sha-256=:") || !strings.HasSuffix(value, ":") {
		return "", ErrInvalid
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(value, "sha-256=:"), ":"))
	if err != nil || len(decoded) != sha256.Size {
		return "", ErrInvalid
	}
	return hex.EncodeToString(decoded), nil
}

func (service *Service) PutPersistent(
	ctx context.Context,
	launchID, capability, idempotencyKey, digestHeader, event string,
	sequence int64,
	body io.Reader,
) (PersistentResult, bool, error) {
	launch, err := service.launch(ctx, launchID, capability)
	if err != nil {
		return PersistentResult{}, false, err
	}
	if launch.persistentSaveMode == "NONE" {
		return PersistentResult{}, false, ErrPersistentUnsupported
	}
	if sequence < 1 || event != "AUTO_INTERVAL" && event != "MANUAL_EXPORT" && event != "EXIT" {
		return PersistentResult{}, false, ErrInvalid
	}
	expectedDigest, err := parseRFC9530(digestHeader)
	if err != nil {
		return PersistentResult{}, false, err
	}
	metadata, err := service.readBounded(body, maxStateBytes)
	if err != nil {
		return PersistentResult{}, false, err
	}
	if subtle.ConstantTimeCompare([]byte(metadata.SHA256), []byte(expectedDigest)) != 1 {
		return PersistentResult{}, false, ErrInvalid
	}
	if err := validatePersistentPayload(launch, metadata); err != nil {
		return PersistentResult{}, false, err
	}
	now := service.now().UnixMilli()
	requestDigestBytes := sha256.Sum256([]byte(strings.Join([]string{
		launchID, fmt.Sprint(sequence), event, metadata.SHA256,
	}, "\x00")))
	return service.persistPersistent(ctx, persistentWrite{
		launchID: launchID, idempotencyKey: idempotencyKey, event: event,
		requestDigest: hex.EncodeToString(requestDigestBytes[:]), sequence: sequence, now: now,
		launch: launch, metadata: metadata,
	})
}

func validatePersistentPayload(launch launchSnapshot, metadata blobstore.Metadata) error {
	if launch.persistentSaveMode != "FILE_TREE" {
		return nil
	}
	stored, err := os.Open(metadata.Path)
	if err != nil {
		return fmt.Errorf("saves/service: open persistent file tree: %w", err)
	}
	validationErr := validateFileTreeBundle(stored, launch.runtimeCoreID == "ppsspp")
	if closeErr := stored.Close(); validationErr == nil && closeErr != nil {
		return fmt.Errorf("saves/service: close persistent file tree: %w", closeErr)
	}
	return validationErr
}

func (service *Service) persistPersistent(
	ctx context.Context,
	request persistentWrite,
) (PersistentResult, bool, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return PersistentResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if previous, replayed, err := replayPersistentRequest(
		ctx, transaction, request.launch.principalID, request.idempotencyKey, request.requestDigest, request.now,
	); err != nil || replayed {
		return previous, replayed, err
	}
	if previous, replayed, err := service.replayPersistentSequence(ctx, transaction, request); err != nil || replayed {
		return previous, replayed, err
	}
	saveID, currentRevisionID, kind, err := resolvePersistentSave(ctx, transaction, request)
	if err != nil {
		return PersistentResult{}, false, err
	}
	result, err := writePersistentRevision(ctx, transaction, request, saveID, currentRevisionID, kind)
	return result, false, err
}

func writePersistentRevision(
	ctx context.Context,
	transaction *sql.Tx,
	request persistentWrite,
	saveID, currentRevisionID, kind string,
) (PersistentResult, error) {
	blobID, err := blobstore.EnsureRecord(
		ctx, transaction, request.metadata, "application/octet-stream", request.now,
	)
	if err != nil {
		return PersistentResult{}, fmt.Errorf("saves/service: %w", err)
	}
	revisionID, err := uuid.NewV7()
	if err != nil {
		return PersistentResult{}, fmt.Errorf("saves/service: %w", err)
	}
	if currentRevisionID == "" {
		_, err = transaction.ExecContext(
			ctx,
			`
INSERT INTO persistent_saves(id,
profile_id,
game_variant_revision_id,
kind,
current_revision_id,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
1,
?,
?)
`,
			saveID,
			request.launch.profileID,
			request.launch.variantRevisionID,
			kind,
			revisionID.String(),
			request.now,
			request.now,
		)
		if err != nil {
			return PersistentResult{}, fmt.Errorf("saves/service: %w", err)
		}
	}
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO persistent_save_revisions(id,
persistent_save_id,
blob_id,
source_launch_session_id,
client_sequence,
source_event,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?)
`,
		revisionID.String(),
		saveID,
		blobID,
		request.launchID,
		request.sequence,
		request.event,
		request.now,
	)
	if err != nil {
		return PersistentResult{}, fmt.Errorf("saves/service: %w", err)
	}
	if currentRevisionID != "" {
		update, updateErr := transaction.ExecContext(
			ctx,
			`
UPDATE persistent_saves
SET current_revision_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND current_revision_id=?
`,
			revisionID.String(),
			request.now,
			saveID,
			currentRevisionID,
		)
		if updateErr != nil {
			return PersistentResult{}, fmt.Errorf("saves/service: %w", updateErr)
		}
		rows, _ := update.RowsAffected()
		if rows != 1 {
			return PersistentResult{}, ErrPersistentConflict
		}
	}
	result := PersistentResult{
		PersistentSaveID: saveID,
		RevisionID:       revisionID.String(),
		Sequence:         request.sequence,
		CreatedAtMS:      request.now,
	}
	if err := storePersistentIdempotency(
		ctx, transaction, request.launch.principalID, request.idempotencyKey,
		request.requestDigest, result, request.now,
	); err != nil {
		return PersistentResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		return PersistentResult{}, fmt.Errorf("saves/service: %w", err)
	}
	return result, nil
}

func replayPersistentRequest(
	ctx context.Context,
	transaction *sql.Tx,
	principalID, idempotencyKey, requestDigest string,
	now int64,
) (PersistentResult, bool, error) {
	var storedDigest string
	var storedBody []byte
	err := transaction.QueryRowContext(ctx, `
SELECT request_digest,response_body FROM idempotency_records
WHERE operation_id='putRuntimePersistentSave' AND key=? AND principal_id=? AND expires_at_ms>?
`, idempotencyKey, principalID, now).Scan(&storedDigest, &storedBody)
	if errors.Is(err, sql.ErrNoRows) {
		return PersistentResult{}, false, nil
	}
	if err != nil {
		return PersistentResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(storedDigest), []byte(requestDigest)) != 1 {
		return PersistentResult{}, false, ErrIdempotencyReused
	}
	var previous PersistentResult
	if err := json.Unmarshal(storedBody, &previous); err != nil {
		return PersistentResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	return previous, true, nil
}

func (service *Service) replayPersistentSequence(
	ctx context.Context,
	transaction *sql.Tx,
	request persistentWrite,
) (PersistentResult, bool, error) {
	var existing PersistentResult
	var existingEvent, existingDigest string
	err := transaction.QueryRowContext(ctx, `
SELECT p.id,r.id,r.client_sequence,r.source_event,b.sha256,r.created_at_ms
FROM persistent_save_revisions r JOIN persistent_saves p ON p.id=r.persistent_save_id
JOIN blobs b ON b.id=r.blob_id
WHERE r.source_launch_session_id=? AND r.client_sequence=?
`, request.launchID, request.sequence).Scan(
		&existing.PersistentSaveID, &existing.RevisionID, &existing.Sequence,
		&existingEvent, &existingDigest, &existing.CreatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PersistentResult{}, false, nil
	}
	if err != nil {
		return PersistentResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	if existingEvent != request.event ||
		subtle.ConstantTimeCompare([]byte(existingDigest), []byte(request.metadata.SHA256)) != 1 {
		return PersistentResult{}, false, ErrSequenceReused
	}
	if err := storePersistentIdempotency(
		ctx, transaction, request.launch.principalID, request.idempotencyKey,
		request.requestDigest, existing, request.now,
	); err != nil {
		return PersistentResult{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return PersistentResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	return existing, true, nil
}

func resolvePersistentSave(
	ctx context.Context,
	transaction *sql.Tx,
	request persistentWrite,
) (string, string, string, error) {
	var lastSequence int64
	_ = transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(client_sequence),0) FROM persistent_save_revisions
WHERE source_launch_session_id=?
`, request.launchID).Scan(&lastSequence)
	if request.sequence != lastSequence+1 {
		return "", "", "", ErrSequenceGap
	}
	kind := request.launch.persistentSaveKind
	if kind != "CORE_SAVE" && kind != "DOS_OVERLAY" {
		return "", "", "", ErrPersistentUnsupported
	}
	var saveID, currentRevisionID string
	err := transaction.QueryRowContext(ctx, `
SELECT id,current_revision_id FROM persistent_saves
WHERE profile_id=? AND game_variant_revision_id=? AND kind=?
`, request.launch.profileID, request.launch.variantRevisionID, kind).Scan(&saveID, &currentRevisionID)
	base := persistentSaveBase(ctx, transaction, request.launchID, request.sequence)
	switch {
	case errors.Is(err, sql.ErrNoRows) && base != "":
		return "", "", "", ErrPersistentConflict
	case errors.Is(err, sql.ErrNoRows):
		generated, idErr := uuid.NewV7()
		if idErr != nil {
			return "", "", "", fmt.Errorf("saves/service: %w", idErr)
		}
		return generated.String(), "", kind, nil
	case err != nil:
		return "", "", "", fmt.Errorf("saves/service: %w", err)
	case currentRevisionID != base:
		return "", "", "", ErrPersistentConflict
	default:
		return saveID, currentRevisionID, kind, nil
	}
}

func persistentSaveBase(ctx context.Context, transaction *sql.Tx, launchID string, sequence int64) string {
	var base string
	if sequence == 1 {
		_ = transaction.QueryRowContext(ctx, `
SELECT COALESCE(persistent_save_base_revision_id,'') FROM launch_sessions WHERE id=?
`, launchID).Scan(&base)
		return base
	}
	_ = transaction.QueryRowContext(ctx, `
SELECT id FROM persistent_save_revisions WHERE source_launch_session_id=? AND client_sequence=?
`, launchID, sequence-1).Scan(&base)
	return base
}

func storePersistentIdempotency(
	ctx context.Context,
	transaction *sql.Tx,
	principalID string,
	key string,
	requestDigest string,
	result PersistentResult,
	now int64,
) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("saves/service: %w", err)
	}
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO idempotency_records(principal_id,
operation_id,
key,
request_digest,
http_status,
response_headers_json,
response_body,
created_at_ms,
expires_at_ms)
VALUES(?,
'putRuntimePersistentSave',
?,
?,
201,
'{}',
?,
?,
?)
`,
		principalID,
		key,
		requestDigest,
		body,
		now,
		now+int64(24*time.Hour/time.Millisecond),
	)
	if err != nil {
		return fmt.Errorf("store persistent-save idempotency record: %w", err)
	}
	return nil
}

func (service *Service) StateDigest(ctx context.Context, launchID, capability string) (string, error) {
	if _, err := service.launch(ctx, launchID, capability); err != nil {
		return "", err
	}
	var digest string
	err := service.database.QueryRowContext(ctx, `
SELECT b.sha256
FROM launch_sessions l
JOIN save_states s ON s.id=l.save_state_id
JOIN blobs b ON b.id=s.state_blob_id
WHERE l.id=?
AND s.game_variant_revision_id=l.game_variant_revision_id
AND s.core_artifact_id=l.core_artifact_id
AND s.deleted_at_ms IS NULL
`, launchID).
		Scan(&digest)
	if err != nil {
		return "", ErrInvalid
	}
	return digest, nil
}

// Keep imports explicit while retaining a testable decoder interface.
type strictJSONDecoder struct{ *json.Decoder }

func newStrictDecoder(reader io.Reader) *strictJSONDecoder {
	return &strictJSONDecoder{json.NewDecoder(reader)}
}

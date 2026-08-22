package saves

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
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
	ErrCredential     = errors.New("LAUNCH_CREDENTIAL_INVALID")
	ErrInvalid        = errors.New("SAVE_INVALID")
	ErrTooLarge       = errors.New("SAVE_TOO_LARGE")
	ErrSequenceReused = errors.New("SAVE_SEQUENCE_REUSED")
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
	datVersionID, dosEntry                                        sql.NullString
	credentialHash                                                []byte
	state                                                         string
	hardExpiresAtMS                                               int64
	contentFormat                                                 string
	discCount, initialDiscIndex                                   int
}

func (service *Service) launch(ctx context.Context, launchID, capability string) (launchSnapshot, error) {
	var result launchSnapshot
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
content.format_version,
(SELECT count(*) FROM launch_external_files external
 WHERE external.launch_session_id=l.id AND external.kind='DISC'),
l.initial_disc_index
FROM launch_sessions l
JOIN game_variant_revisions r ON r.id=l.game_variant_revision_id
JOIN launch_content_files content ON content.launch_session_id=l.id
LEFT JOIN users u ON u.profile_id=l.profile_id
WHERE l.id=?
`, launchID).
		Scan(
			&result.principalID, &result.profileID, &result.gameID, &result.variantRevisionID, &result.artifactID,
			&result.datVersionID, &result.dosEntry, &result.credentialHash, &result.state, &result.hardExpiresAtMS,
			&result.contentFormat, &result.discCount, &result.initialDiscIndex,
		)
	if err != nil || !retromruntime.MatchesCapability(capability, result.credentialHash) ||
		result.state != "ACTIVE" || result.hardExpiresAtMS <= service.now().UnixMilli() {
		return launchSnapshot{}, ErrCredential
	}
	if !validLaunchDiscShape(result) {
		return launchSnapshot{}, ErrCredential
	}
	return result, nil
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

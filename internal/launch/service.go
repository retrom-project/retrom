package launch

import (
	"archive/zip"
	"compress/flate"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
)

var (
	ErrBlocked         = errors.New("LAUNCH_BLOCKED")
	ErrCredential      = errors.New("LAUNCH_CREDENTIAL_INVALID")
	ErrDOSEntryMissing = errors.New("LAUNCH_DOS_ENTRY_MISSING")
	ErrDOSEntryUnsafe  = errors.New("LAUNCH_DOS_ENTRY_UNSAFE")
)

type Capabilities struct {
	SecureContext       bool `json:"secureContext"`
	CrossOriginIsolated bool `json:"crossOriginIsolated"`
	SharedArrayBuffer   bool `json:"sharedArrayBuffer"`
}

type CreateRequest struct {
	GameID             string       `json:"gameId"`
	CoreID             *string      `json:"coreId"`
	SaveStateID        *string      `json:"saveStateId"`
	DOSEntry           *string      `json:"dosEntry"`
	ReturnTo           string       `json:"returnTo"`
	ClientCapabilities Capabilities `json:"clientCapabilities"`
}

type Created struct {
	Status               string   `json:"status,omitempty"`
	JobID                string   `json:"jobId,omitempty"`
	RetryAfterMS         int64    `json:"retryAfterMs,omitempty"`
	LaunchID             string   `json:"launchId"`
	PlayURL              string   `json:"playUrl"`
	Warnings             []string `json:"warnings"`
	BootstrapExpiresAtMS int64    `json:"bootstrapExpiresAtMs"`
	HardExpiresAtMS      int64    `json:"hardExpiresAtMs"`
	Capability           string   `json:"-"`
}

type Service struct {
	database     *sql.DB
	blobs        *blobstore.Store
	dependencies *dependencies.Set
	credentials  *retromruntime.Credentials
	now          func() time.Time
}

func New(
	database *sql.DB,
	dependencySet *dependencies.Set,
	credentials *retromruntime.Credentials,
	now func() time.Time,
) *Service {
	return &Service{database: database, dependencies: dependencySet, credentials: credentials, now: now}
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Create(ctx context.Context, request CreateRequest) (Created, error) {
	if request.GameID == "" || !validReturnTo(request.ReturnTo, request.GameID) {
		return Created{}, ErrBlocked
	}
	coreID := ""
	if request.CoreID != nil {
		coreID = *request.CoreID
	}
	var variantRevisionID, artifactID, selectedCore, emulatorVersion string
	var requiresThreads int
	var savedDOSEntry sql.NullString
	if request.SaveStateID != nil {
		if request.DOSEntry != nil {
			return Created{}, ErrBlocked
		}
		err := service.database.QueryRowContext(ctx, `
SELECT s.game_variant_revision_id,
s.core_artifact_id,
a.core_id,
a.emulatorjs_version,
c.requires_threads,
s.dos_entry_path
FROM save_states s
JOIN games g ON g.id=s.game_id
JOIN game_variant_revisions r ON r.id=s.game_variant_revision_id
AND r.core_artifact_id=s.core_artifact_id
JOIN core_artifacts a ON a.id=s.core_artifact_id
JOIN cores c ON c.id=a.core_id
WHERE s.id=?
AND s.game_id=?
AND s.profile_id='local'
AND s.deleted_at_ms IS NULL
AND g.status='PUBLISHED'
AND r.status='READY'
`, *request.SaveStateID, request.GameID).
			Scan(&variantRevisionID, &artifactID, &selectedCore, &emulatorVersion, &requiresThreads, &savedDOSEntry)
		if err != nil || request.CoreID != nil && coreID != selectedCore {
			return Created{}, ErrBlocked
		}
	} else {
		query := `
SELECT v.current_revision_id,
r.core_artifact_id,
a.core_id,
a.emulatorjs_version,
c.requires_threads
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN game_variants v ON v.game_id=g.id
JOIN game_variant_revisions r ON r.id=v.current_revision_id
AND r.game_content_revision_id=g.current_content_revision_id
JOIN core_artifacts a ON a.id=r.core_artifact_id
JOIN cores c ON c.id=a.core_id
WHERE g.id=?
AND g.status='PUBLISHED'
AND r.status='READY'
AND v.core_id=CASE WHEN ?='' THEN pi.default_core_id ELSE ? END
`
		if err := service.database.QueryRowContext(ctx, query, request.GameID, coreID, coreID).Scan(
			&variantRevisionID,
			&artifactID,
			&selectedCore,
			&emulatorVersion,
			&requiresThreads,
		); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return service.ensureVariant(ctx, request, coreID, true)
			}
			return Created{}, ErrBlocked
		}
	}
	if requiresThreads == 1 &&
		(!request.ClientCapabilities.SecureContext ||
			!request.ClientCapabilities.CrossOriginIsolated ||
			!request.ClientCapabilities.SharedArrayBuffer) {
		return Created{}, ErrBlocked
	}
	if service.dependencies.Versions[emulatorVersion] == nil {
		return Created{}, ErrBlocked
	}
	selectedDOSEntry := request.DOSEntry
	if request.SaveStateID != nil && savedDOSEntry.Valid {
		selectedDOSEntry = &savedDOSEntry.String
	}
	if selectedDOSEntry != nil {
		var directLaunchSafe int
		err := service.database.QueryRowContext(ctx, `
SELECT d.direct_launch_safe
FROM game_variant_revisions r
JOIN dos_entries d ON d.game_content_revision_id=r.game_content_revision_id
WHERE r.id=?
AND d.normalized_path=?
AND d.enabled=1
`, variantRevisionID, *selectedDOSEntry).
			Scan(&directLaunchSafe)
		if errors.Is(err, sql.ErrNoRows) {
			return Created{}, ErrDOSEntryMissing
		}
		if err != nil {
			return Created{}, fmt.Errorf("launch/service: %w", err)
		}
		if directLaunchSafe != 1 {
			return Created{}, ErrDOSEntryUnsafe
		}
	}
	contentBlobID, contentLogicalName, contentFormat, err := service.lockLaunchContent(
		ctx,
		variantRevisionID,
		selectedCore,
		selectedDOSEntry,
	)
	if err != nil {
		return Created{}, err
	}
	if err := service.validateLaunchLogicalNames(ctx, variantRevisionID, contentLogicalName); err != nil {
		return Created{}, err
	}
	kind := "CORE_SAVE"
	if selectedCore == "dosbox_pure" {
		kind = "DOS_OVERLAY"
	}
	var persistentBase sql.NullString
	baseErr := service.database.QueryRowContext(ctx, `
SELECT current_revision_id
FROM persistent_saves
WHERE profile_id='local'
AND game_variant_revision_id=?
AND kind=?
`, variantRevisionID, kind).
		Scan(&persistentBase)
	if baseErr != nil && !errors.Is(baseErr, sql.ErrNoRows) {
		return Created{}, fmt.Errorf("launch/service: %w", baseErr)
	}
	launchID, err := uuid.NewV7()
	if err != nil {
		return Created{}, fmt.Errorf("launch/service: %w", err)
	}
	capability := service.credentials.Capability(launchID)
	capabilityHash := retromruntime.HashCapability(capability)
	now := service.now().UnixMilli()
	bootstrapExpires := now + int64(5*time.Minute/time.Millisecond)
	hardExpires := now + int64(24*time.Hour/time.Millisecond)
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("launch/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO launch_sessions(id,
profile_id,
game_id,
game_variant_revision_id,
core_artifact_id,
save_state_id,
dos_entry_path,
persistent_save_base_revision_id,
return_to,
credential_sha256,
state,
bootstrap_expires_at_ms,
hard_expires_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'local',
?,
?,
?,
?,
?,
?,
?,
?,
'CREATED',
?,
?,
?,
?)
`,
		launchID.String(),
		request.GameID,
		variantRevisionID,
		artifactID,
		request.SaveStateID,
		selectedDOSEntry,
		persistentBase,
		request.ReturnTo,
		capabilityHash[:],
		bootstrapExpires,
		hardExpires,
		now,
		now,
	)
	if err != nil {
		return Created{}, fmt.Errorf("create launch session: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_content_files(launch_session_id,
logical_name,
blob_id,
format_version,
created_at_ms) VALUES(?,
?,
?,
?,
?)
`, launchID.String(), contentLogicalName, contentBlobID, contentFormat, now); err != nil {
		return Created{}, fmt.Errorf("lock launch content: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("commit launch session: %w", err)
	}
	return Created{
		LaunchID:             launchID.String(),
		PlayURL:              "/play/" + launchID.String(),
		Warnings:             []string{},
		BootstrapExpiresAtMS: bootstrapExpires,
		HardExpiresAtMS:      hardExpires,
		Capability:           retromruntime.EncodeCapability(capability),
	}, nil
}

func (service *Service) validateLaunchLogicalNames(
	ctx context.Context,
	variantRevisionID, contentLogicalName string,
) error {
	seen := map[string]struct{}{strings.ToLower(contentLogicalName): {}}
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT logical_name
FROM variant_files
WHERE game_variant_revision_id=?
AND role IN ('PARENT',
'BIOS_BUNDLE')
ORDER BY role,
sort_order,
logical_name
`,
		variantRevisionID,
	)
	if err != nil {
		return fmt.Errorf("launch/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var logicalName string
		if err := rows.Scan(&logicalName); err != nil {
			return fmt.Errorf("launch/service: %w", err)
		}
		key := strings.ToLower(logicalName)
		if _, exists := seen[key]; exists || logicalName == "" || path.Base(logicalName) != logicalName ||
			strings.Contains(logicalName, `\`) {
			return ErrBlocked
		}
		seen[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan launch content files: %w", err)
	}
	return nil
}

func (service *Service) lockLaunchContent(
	ctx context.Context,
	variantRevisionID, coreID string,
	dosEntry *string,
) (string, string, string, error) {
	if coreID != "dosbox_pure" {
		var blobID, logicalName string
		err := service.database.QueryRowContext(ctx, `
SELECT f.blob_id,
f.logical_name
FROM game_variant_revisions r
JOIN game_content_files f ON f.game_content_revision_id=r.game_content_revision_id
AND f.role='CONTENT'
WHERE r.id=?
ORDER BY f.sort_order,
f.logical_name LIMIT 1
`, variantRevisionID).
			Scan(&blobID, &logicalName)
		if err != nil {
			return "", "", "", ErrBlocked
		}
		return blobID, logicalName, "SOURCE_V1", nil
	}
	var baseBlobID, baseDigest string
	err := service.database.QueryRowContext(ctx, `
SELECT vf.blob_id,
b.sha256
FROM variant_files vf
JOIN blobs b ON b.id=vf.blob_id
WHERE vf.game_variant_revision_id=?
AND vf.role='DOS_LAUNCH_BUNDLE'
AND vf.logical_name='game.zip'
`, variantRevisionID).
		Scan(&baseBlobID, &baseDigest)
	if err != nil {
		return "", "", "", ErrBlocked
	}
	if dosEntry == nil {
		return baseBlobID, "game.zip", "SOURCE_V1", nil
	}
	if service.blobs == nil {
		return "", "", "", ErrBlocked
	}
	metadata, err := service.buildDOSDirectBundle(baseDigest, *dosEntry)
	if err != nil {
		return "", "", "", err
	}
	blobID, err := blobstore.EnsureRecord(ctx, service.database, metadata, "application/zip", service.now().UnixMilli())
	if err != nil {
		return "", "", "", fmt.Errorf("launch/service: %w", err)
	}
	entryDigest := sha256.Sum256([]byte(*dosEntry))
	return blobID, fmt.Sprintf("game-%x.zip", entryDigest[:8]), "RETROM_DOS_DIRECT_ZIP_V1", nil
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) buildDOSDirectBundle(baseDigest, selectedEntry string) (blobstore.Metadata, error) {
	base, err := service.blobs.OpenDigest(baseDigest)
	if err != nil {
		return blobstore.Metadata{}, ErrBlocked
	}
	defer func() { cleanup.Error("close", base.Close()) }()
	stat, err := base.Stat()
	if err != nil {
		return blobstore.Metadata{}, ErrBlocked
	}
	archive, err := zip.NewReader(base, stat.Size())
	if err != nil {
		return blobstore.Metadata{}, ErrBlocked
	}
	files := make(map[string]*zip.File, len(archive.File))
	found := false
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || strings.EqualFold(file.Name, "dosbox.conf") {
			return blobstore.Metadata{}, ErrBlocked
		}
		files[file.Name] = file
		if file.Name == selectedEntry {
			found = true
		}
	}
	if !found {
		return blobstore.Metadata{}, ErrDOSEntryMissing
	}
	names := make([]string, 0, len(files)+1)
	for name := range files {
		names = append(names, name)
	}
	names = append(names, "dosbox.conf")
	sort.Strings(names)
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		output := zip.NewWriter(writer)
		output.RegisterCompressor(zip.Deflate, func(destination io.Writer) (io.WriteCloser, error) {
			return flate.NewWriter(destination, 6)
		})
		var buildErr error
		for _, name := range names {
			header := &zip.FileHeader{Name: name, Method: zip.Deflate}
			header.SetMode(0o644)
			// archive/zip otherwise injects an extended-timestamp extra field. The
			// legacy DOS fields are the only public API that can encode the required
			// 1980 epoch while keeping Extra byte-for-byte empty.
			header.ModifiedDate = 33 //nolint:staticcheck // See deterministic ZIP rationale above.
			header.ModifiedTime = 0  //nolint:staticcheck // See deterministic ZIP rationale above.
			destination, createErr := output.CreateHeader(header)
			if createErr != nil {
				buildErr = createErr
				break
			}
			if name == "dosbox.conf" {
				_, buildErr = io.WriteString(destination, dosboxConfig(selectedEntry))
			} else {
				source, openErr := files[name].Open()
				if openErr != nil {
					buildErr = openErr
					break
				}
				_, buildErr = io.Copy(destination, source)
				closeErr := source.Close()
				if buildErr == nil {
					buildErr = closeErr
				}
			}
			if buildErr != nil {
				break
			}
		}
		closeErr := output.Close()
		if buildErr == nil {
			buildErr = closeErr
		}
		_ = writer.CloseWithError(buildErr)
		done <- buildErr
	}()
	metadata, putErr := service.blobs.Put(reader)
	buildErr := <-done
	if putErr != nil {
		return blobstore.Metadata{}, fmt.Errorf("launch/service: %w", putErr)
	}
	if buildErr != nil {
		return blobstore.Metadata{}, buildErr
	}
	return metadata, nil
}

func dosboxConfig(selectedEntry string) string {
	directory, executable := path.Split(selectedEntry)
	directory = strings.TrimSuffix(directory, "/")
	changeDirectory := `CD \`
	if directory != "" {
		changeDirectory = `CD "\` + strings.ReplaceAll(directory, "/", `\`) + `"`
	}
	return "[autoexec]\r\n@ECHO OFF\r\nC:\r\n" + changeDirectory + "\r\n\"" + executable + "\"\r\n"
}

func validReturnTo(value, gameID string) bool {
	if strings.ContainsAny(value, "?#%\\") {
		return false
	}
	return value == "/" || value == "/library" || value == "/saves" || value == "/games/"+gameID
}

type Config struct {
	LaunchID             string            `json:"launchId"`
	EmulatorJSVersion    string            `json:"emulatorjsVersion"`
	PlayerAdapterID      string            `json:"playerAdapterId"`
	Core                 string            `json:"core"`
	CoreArtifactID       string            `json:"coreArtifactId"`
	EmulatorGameID       int64             `json:"emulatorGameId"`
	GameName             string            `json:"gameName"`
	RuntimeBaseURL       string            `json:"runtimeBaseUrl"`
	LoaderURL            string            `json:"loaderUrl"`
	GameURL              string            `json:"gameUrl"`
	BIOSURL              any               `json:"biosUrl"`
	ParentURL            any               `json:"parentUrl"`
	StateURL             any               `json:"stateUrl"`
	PersistentSaveURL    string            `json:"persistentSaveUrl"`
	RequiresThreads      bool              `json:"requiresThreads"`
	RuntimePathOverrides map[string]string `json:"runtimePathOverrides"`
	DefaultCoreOptions   map[string]string `json:"defaultCoreOptions"`
	DOSEntry             any               `json:"dosEntry"`
	Warnings             []string          `json:"warnings"`
	ReturnTo             string            `json:"returnTo"`
}

type BundleFile struct {
	LogicalName string
	SHA256      string
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Config(ctx context.Context, launchID, capability string) (Config, error) {
	var credentialHash []byte
	var state, coreID, artifactID, emulatorVersion, relativePath, compatibilityJSON, logicalName, returnTo string
	var bootstrapExpires, hardExpires, emulatorGameID int64
	var requiresThreads int
	var saveStateID, dosEntry sql.NullString
	var idleExpires sql.NullInt64
	err := service.database.QueryRowContext(ctx, `
SELECT l.credential_sha256,
l.state,
l.bootstrap_expires_at_ms,
l.hard_expires_at_ms,
l.idle_expires_at_ms,
a.core_id,
a.id,
a.emulatorjs_version,
a.relative_path,
a.compatibility_config_json,
c.requires_threads,
r.emulator_game_id,
lc.logical_name,
l.return_to,
l.save_state_id,
l.dos_entry_path
FROM launch_sessions l
JOIN core_artifacts a ON a.id=l.core_artifact_id
JOIN cores c ON c.id=a.core_id
JOIN game_variant_revisions r ON r.id=l.game_variant_revision_id
JOIN launch_content_files lc ON lc.launch_session_id=l.id
WHERE l.id=?
`, launchID).
		Scan(
			&credentialHash,
			&state,
			&bootstrapExpires,
			&hardExpires,
			&idleExpires,
			&coreID,
			&artifactID,
			&emulatorVersion,
			&relativePath,
			&compatibilityJSON,
			&requiresThreads,
			&emulatorGameID,
			&logicalName,
			&returnTo,
			&saveStateID,
			&dosEntry,
		)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) {
		return Config{}, ErrCredential
	}
	now := service.now().UnixMilli()
	if hardExpires <= now || state == "CREATED" && bootstrapExpires <= now ||
		idleExpires.Valid && idleExpires.Int64 <= now ||
		state == "FINISHED" ||
		state == "EXPIRED" ||
		state == "REVOKED" {
		return Config{}, ErrCredential
	}
	if state == "CREATED" {
		if _, err := service.database.ExecContext(ctx, `
UPDATE launch_sessions
SET state='ACTIVE',
activated_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
AND state='CREATED'
`, now, now, launchID); err != nil {
			return Config{}, fmt.Errorf("launch/service: %w", err)
		}
	}
	version := service.dependencies.Versions[emulatorVersion]
	if version == nil {
		return Config{}, ErrCredential
	}
	base := "/runtime/emulatorjs/" + emulatorVersion + "/"
	var compatibility struct {
		RequestedArtifactBasename string `json:"requestedArtifactBasename"`
	}
	if err := json.Unmarshal([]byte(compatibilityJSON), &compatibility); err != nil ||
		compatibility.RequestedArtifactBasename == "" {
		return Config{}, ErrCredential
	}
	overrides := map[string]string{compatibility.RequestedArtifactBasename: base + relativePath}
	stateURL := any(nil)
	if saveStateID.Valid {
		stateURL = "/runtime/launches/" + launchID + "/state"
	}
	biosURL, parentURL := any(nil), any(nil)
	biosFiles, _ := service.BundleFiles(ctx, launchID, capability, "BIOS_BUNDLE")
	parentFiles, _ := service.BundleFiles(ctx, launchID, capability, "PARENT")
	if len(biosFiles) > 0 {
		biosURL = "/runtime/launches/" + launchID + "/bios/bundle.zip"
	}
	if len(parentFiles) > 0 {
		parentURL = "/runtime/launches/" + launchID + "/parent/bundle.zip"
	}
	coreOptions := map[string]string{"webgl2Enabled": "enabled"}
	warnings := make([]string, 0)
	optionRows, optionErr := service.database.QueryContext(
		ctx,
		`
SELECT q.condition_code,
q.activation_options_json,
i.status
FROM launch_sessions l
JOIN bios_requirements q ON q.core_artifact_id=l.core_artifact_id
AND q.enabled=1
JOIN bios_installations i ON i.requirement_id=q.id
AND i.is_active=1
AND i.status IN ('MATCHED',
'HASH_WARNING')
WHERE l.id=?
AND q.activation_options_json IS NOT NULL
ORDER BY q.logical_name
`,
		launchID,
	)
	if optionErr != nil {
		return Config{}, fmt.Errorf("launch/service: %w", optionErr)
	}
	defer func() { cleanup.Error("close", optionRows.Close()) }()
	for optionRows.Next() {
		var condition, optionsJSON, installationStatus string
		if err := optionRows.Scan(&condition, &optionsJSON, &installationStatus); err != nil {
			return Config{}, fmt.Errorf("launch/service: %w", err)
		}
		if !biosApplies(condition, logicalName) {
			continue
		}
		var options map[string]string
		if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
			return Config{}, ErrBlocked
		}
		for name, value := range options {
			if existing, ok := coreOptions[name]; ok && existing != value {
				return Config{}, ErrBlocked
			}
			coreOptions[name] = value
		}
		if installationStatus == "HASH_WARNING" {
			warnings = append(warnings, "BIOS_HASH_WARNING")
		}
	}
	if err := optionRows.Err(); err != nil {
		return Config{}, fmt.Errorf("launch/service: %w", err)
	}
	if coreID == "dosbox_pure" && dosEntry.Valid {
		if existing, ok := coreOptions["dosbox_pure_conf"]; ok && existing != "inside" {
			return Config{}, ErrBlocked
		}
		coreOptions["dosbox_pure_conf"] = "inside"
	}
	return Config{
		LaunchID:          launchID,
		EmulatorJSVersion: emulatorVersion,
		PlayerAdapterID:   version.Manifest.EmulatorJS.PlayerAdapter.ID,
		Core:              coreID,
		CoreArtifactID:    artifactID,
		EmulatorGameID:    emulatorGameID,
		GameName:          fmt.Sprintf("retrom-%d", emulatorGameID),
		RuntimeBaseURL: base + strings.TrimSuffix(
			version.Manifest.EmulatorJS.PlayerAdapter.RuntimeBasePath,
			"/",
		) + "/",
		LoaderURL:            base + version.Manifest.EmulatorJS.PlayerAdapter.LoaderPath,
		GameURL:              "/runtime/launches/" + launchID + "/game/" + url.PathEscape(logicalName),
		BIOSURL:              biosURL,
		ParentURL:            parentURL,
		StateURL:             stateURL,
		PersistentSaveURL:    "/runtime/launches/" + launchID + "/persistent-save",
		RequiresThreads:      requiresThreads == 1,
		RuntimePathOverrides: overrides,
		DefaultCoreOptions:   coreOptions,
		DOSEntry:             nullableString(dosEntry),
		Warnings:             warnings,
		ReturnTo:             returnTo,
	}, nil
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) BundleFiles(ctx context.Context, launchID, capability, kind string) ([]BundleFile, error) {
	if kind != "BIOS_BUNDLE" && kind != "PARENT" {
		return nil, ErrCredential
	}
	var credentialHash []byte
	var state, contentName string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT l.credential_sha256,
l.state,
l.hard_expires_at_ms,
lc.logical_name
FROM launch_sessions l
JOIN launch_content_files lc ON lc.launch_session_id=l.id
WHERE l.id=?
`, launchID).
		Scan(&credentialHash, &state, &hardExpires, &contentName)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= service.now().UnixMilli() ||
		state != "ACTIVE" {
		return nil, ErrCredential
	}
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT vf.logical_name,
b.sha256
FROM launch_sessions l
JOIN variant_files vf ON vf.game_variant_revision_id=l.game_variant_revision_id
JOIN blobs b ON b.id=vf.blob_id
WHERE l.id=?
AND vf.role=?
ORDER BY vf.sort_order,
vf.logical_name
`,
		launchID,
		kind,
	)
	if err != nil {
		return nil, fmt.Errorf("launch/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]BundleFile, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var file BundleFile
		if err := rows.Scan(&file.LogicalName, &file.SHA256); err != nil {
			return nil, fmt.Errorf("launch/service: %w", err)
		}
		if _, duplicate := seen[file.LogicalName]; !duplicate {
			files = append(files, file)
			seen[file.LogicalName] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("launch/service: %w", err)
	}
	if kind == "BIOS_BUNDLE" {
		biosRows, err := service.database.QueryContext(
			ctx,
			`
SELECT q.logical_name,
q.condition_code,
q.activation_options_json,
b.sha256
FROM launch_sessions l
JOIN bios_requirements q ON q.core_artifact_id=l.core_artifact_id
AND q.enabled=1
JOIN bios_installations i ON i.requirement_id=q.id
AND i.is_active=1
AND i.status IN ('MATCHED',
'HASH_WARNING')
JOIN blobs b ON b.id=i.blob_id
WHERE l.id=?
ORDER BY q.logical_name
`,
			launchID,
		)
		if err != nil {
			return nil, fmt.Errorf("launch/service: %w", err)
		}
		defer func() { cleanup.Error("close", biosRows.Close()) }()
		for biosRows.Next() {
			var file BundleFile
			var condition string
			var options sql.NullString
			if err := biosRows.Scan(&file.LogicalName, &condition, &options, &file.SHA256); err != nil {
				return nil, fmt.Errorf("launch/service: %w", err)
			}
			if !biosApplies(condition, contentName) {
				continue
			}
			if _, duplicate := seen[file.LogicalName]; !duplicate {
				files = append(files, file)
				seen[file.LogicalName] = struct{}{}
			}
		}
		if err := biosRows.Err(); err != nil {
			return nil, fmt.Errorf("launch/service: %w", err)
		}
	}
	slices.SortFunc(
		files,
		func(left, right BundleFile) int { return strings.Compare(left.LogicalName, right.LogicalName) },
	)
	return files, nil
}

func biosApplies(condition, contentName string) bool {
	extension := strings.ToLower(path.Ext(contentName))
	switch condition {
	case "FDS_CONTENT":
		return extension == ".fds"
	case "GB_CONTENT":
		return extension == ".gb" || extension == ".dmg"
	case "GBC_CONTENT":
		return extension == ".gbc"
	case "GBA_CONTENT":
		return extension == ".gba"
	case "GAME_GENIE_ADDON_MODE", "MGBA_SGB_MODEL":
		return false
	default:
		return true
	}
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func (service *Service) ContentBlob(ctx context.Context, launchID, capability, logicalName string) (string, error) {
	var credentialHash []byte
	var digest, state string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT l.credential_sha256,
l.state,
l.hard_expires_at_ms,
b.sha256
FROM launch_sessions l
JOIN launch_content_files lc ON lc.launch_session_id=l.id
JOIN blobs b ON b.id=lc.blob_id
WHERE l.id=?
AND lc.logical_name=?
`, launchID, logicalName).Scan(&credentialHash, &state, &hardExpires, &digest)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= service.now().UnixMilli() ||
		state != "ACTIVE" {
		return "", ErrCredential
	}
	return digest, nil
}

type Interval struct {
	Running bool `json:"running"`
	Visible bool `json:"visible"`
	Paused  bool `json:"paused"`
}

type PlayEvent struct {
	ClientSequence     int64     `json:"clientSequence"`
	ClientObservedAtMS int64     `json:"clientObservedAtMs"`
	PreviousInterval   *Interval `json:"previousInterval"`
}

type PlayResult struct {
	PlaySessionID    any    `json:"playSessionId"`
	ClientSequence   int64  `json:"clientSequence"`
	AcceptedDuration int64  `json:"acceptedDurationMs"`
	State            string `json:"state"`
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) RecordPlay(
	ctx context.Context,
	launchID, capability, kind string,
	event PlayEvent,
) (PlayResult, error) {
	if event.ClientObservedAtMS < 0 || event.ClientObservedAtMS > 253402300799999 {
		return PlayResult{}, ErrBlocked
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var credentialHash []byte
	var launchState, profileID, gameID, variantRevisionID string
	var hardExpires int64
	var idleExpires sql.NullInt64
	now := service.now().UnixMilli()
	if err := transaction.QueryRowContext(ctx, `
SELECT credential_sha256,
state,
profile_id,
game_id,
game_variant_revision_id,
hard_expires_at_ms,
idle_expires_at_ms
FROM launch_sessions
WHERE id=?
`, launchID).Scan(
		&credentialHash,
		&launchState,
		&profileID,
		&gameID,
		&variantRevisionID,
		&hardExpires,
		&idleExpires,
	); err != nil ||
		!retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= now {
		return PlayResult{}, ErrCredential
	}
	var playID, playState string
	var lastSequence, lastHeartbeat int64
	err = transaction.QueryRowContext(ctx, `
SELECT id,
state,
last_client_sequence,
last_heartbeat_at_ms
FROM play_sessions
WHERE launch_session_id=?
`, launchID).
		Scan(&playID, &playState, &lastSequence, &lastHeartbeat)
	if err == nil && event.ClientSequence <= lastSequence {
		var storedKind string
		var storedObserved, accepted int64
		var running, visible, paused bool
		if replayErr := transaction.QueryRowContext(ctx, `
SELECT event_kind,
client_observed_at_ms,
running,
visible,
paused,
accepted_duration_ms
FROM play_session_events
WHERE play_session_id=?
AND client_sequence=?
`, playID, event.ClientSequence).Scan(
			&storedKind,
			&storedObserved,
			&running,
			&visible,
			&paused,
			&accepted,
		); replayErr != nil {
			return PlayResult{}, ErrBlocked
		}
		expectedKind := "HEARTBEAT"
		switch kind {
		case "start":
			expectedKind = "START"
		case "finish":
			expectedKind = "FINISH"
		}
		intervalMatches := event.PreviousInterval == nil && storedKind == "START" ||
			event.PreviousInterval != nil && running == event.PreviousInterval.Running &&
				visible == event.PreviousInterval.Visible &&
				paused == event.PreviousInterval.Paused
		if storedKind != expectedKind || storedObserved != event.ClientObservedAtMS || !intervalMatches {
			return PlayResult{}, ErrBlocked
		}
		state := "ACTIVE"
		if storedKind == "FINISH" {
			state = "FINISHED"
		}
		return PlayResult{
			PlaySessionID:    playID,
			ClientSequence:   event.ClientSequence,
			AcceptedDuration: accepted,
			State:            state,
		}, nil
	}
	if kind == "start" {
		if event.ClientSequence != 0 || event.PreviousInterval != nil || launchState != "ACTIVE" {
			return PlayResult{}, ErrBlocked
		}
		if err == nil {
			return PlayResult{PlaySessionID: playID, ClientSequence: 0, AcceptedDuration: 0, State: playState}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		generated, _ := uuid.NewV7()
		playID = generated.String()
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO play_sessions(id,
launch_session_id,
profile_id,
game_id,
game_variant_revision_id,
started_at_ms,
last_heartbeat_at_ms,
active_duration_ms,
last_client_sequence,
state,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
0,
0,
'ACTIVE',
1,
?,
?)
`, playID, launchID, profileID, gameID, variantRevisionID, now, now, now, now); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO play_session_events(play_session_id,
client_sequence,
event_kind,
client_observed_at_ms,
server_received_at_ms,
running,
visible,
paused,
accepted_duration_ms,
created_at_ms) VALUES(?,
0,
'START',
?,
?,
0,
0,
0,
0,
?)
`, playID, event.ClientObservedAtMS, now, now); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET idle_expires_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`, now+int64(2*time.Minute/time.Millisecond), now, launchID); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		if err := transaction.Commit(); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		return PlayResult{PlaySessionID: playID, ClientSequence: 0, AcceptedDuration: 0, State: "ACTIVE"}, nil
	}
	if errors.Is(err, sql.ErrNoRows) && kind == "finish" && event.ClientSequence == 0 && event.PreviousInterval == nil {
		if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET state='FINISHED',
finished_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
AND state='ACTIVE'
`, now, now, launchID); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		if err := transaction.Commit(); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		return PlayResult{PlaySessionID: nil, ClientSequence: 0, AcceptedDuration: 0, State: "FINISHED"}, nil
	}
	if err != nil || playState != "ACTIVE" || launchState != "ACTIVE" ||
		idleExpires.Valid && idleExpires.Int64 <= now ||
		event.PreviousInterval == nil ||
		event.ClientSequence != lastSequence+1 ||
		(kind != "heartbeat" && kind != "finish") {
		return PlayResult{}, ErrBlocked
	}
	accepted := int64(0)
	if event.PreviousInterval.Running && event.PreviousInterval.Visible && !event.PreviousInterval.Paused {
		accepted = min(now-lastHeartbeat, int64(45*time.Second/time.Millisecond))
		if accepted < 0 {
			accepted = 0
		}
	}
	eventKind := "HEARTBEAT"
	newState := "ACTIVE"
	var endedAt any
	if kind == "finish" {
		eventKind, newState, endedAt = "FINISH", "FINISHED", now
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO play_session_events(play_session_id,
client_sequence,
event_kind,
client_observed_at_ms,
server_received_at_ms,
running,
visible,
paused,
accepted_duration_ms,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
		playID,
		event.ClientSequence,
		eventKind,
		event.ClientObservedAtMS,
		now,
		event.PreviousInterval.Running,
		event.PreviousInterval.Visible,
		event.PreviousInterval.Paused,
		accepted,
		now,
	); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE play_sessions
SET last_heartbeat_at_ms=?,
ended_at_ms=?,
active_duration_ms=active_duration_ms+?,
last_client_sequence=?,
state=?,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now, endedAt, accepted, event.ClientSequence, newState, now, playID); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if kind == "finish" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET state='FINISHED',
finished_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`, now, now, launchID); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
	} else if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET idle_expires_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`, now+int64(2*time.Minute/time.Millisecond), now, launchID); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	return PlayResult{
		PlaySessionID:    playID,
		ClientSequence:   event.ClientSequence,
		AcceptedDuration: accepted,
		State:            newState,
	}, nil
}

func MarshalConfig(config Config) ([]byte, error) {
	contents, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal launch config: %w", err)
	}
	return contents, nil
}

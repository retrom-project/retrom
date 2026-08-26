package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
)

type Config struct {
	RuntimeFamily        string                       `json:"runtimeFamily"`
	Mode                 string                       `json:"mode"`
	LaunchID             string                       `json:"launchId"`
	EmulatorJSVersion    string                       `json:"emulatorjsVersion"`
	PlayerAdapterID      string                       `json:"playerAdapterId"`
	Core                 string                       `json:"core"`
	RuntimeCore          string                       `json:"runtimeCore"`
	CoreName             string                       `json:"coreName"`
	CoreArtifactID       string                       `json:"coreArtifactId"`
	EmulatorGameID       int64                        `json:"emulatorGameId"`
	GameName             string                       `json:"gameName"`
	GameTitle            string                       `json:"gameTitle"`
	PlatformName         string                       `json:"platformName"`
	RuntimeBaseURL       string                       `json:"runtimeBaseUrl"`
	LoaderURL            string                       `json:"loaderUrl"`
	GameURL              string                       `json:"gameUrl"`
	BIOSURL              any                          `json:"biosUrl"`
	ParentURL            any                          `json:"parentUrl"`
	StateURL             any                          `json:"stateUrl"`
	InputMode            string                       `json:"inputMode"`
	StartupActions       []dependencies.StartupAction `json:"startupActions"`
	RequiresThreads      bool                         `json:"requiresThreads"`
	RuntimePathOverrides map[string]string            `json:"runtimePathOverrides"`
	DefaultCoreOptions   map[string]string            `json:"defaultCoreOptions"`
	ExternalFiles        map[string]string            `json:"externalFiles"`
	DiscSet              *DiscSet                     `json:"discSet"`
	DOSEntry             any                          `json:"dosEntry"`
	Warnings             []string                     `json:"warnings"`
	ReturnTo             string                       `json:"returnTo"`
	ReviewPreview        *ReviewPreviewConfig         `json:"reviewPreview,omitempty"`
	Netplay              *NetplayConfig               `json:"netplay"`
	RPGMaker             *RPGMakerConfig              `json:"-"`
}

type NetplayConfig struct {
	RoomID           string          `json:"roomId"`
	SessionID        string          `json:"sessionId"`
	PlayerNo         int             `json:"playerNo"`
	NetplayProfile   json.RawMessage `json:"netplayProfile"`
	RuntimeSocketURL string          `json:"runtimeSocketUrl"`
}

type DiscSet struct {
	ContentKind      string      `json:"contentKind"`
	Count            int         `json:"count"`
	InitialDiscIndex int         `json:"initialDiscIndex"`
	Entries          []DiscEntry `json:"entries"`
}

type DiscEntry struct {
	Index       int    `json:"index"`
	Label       string `json:"label"`
	VirtualPath string `json:"virtualPath"`
}

type MultiDiscTelemetryDimensions struct {
	PlatformKey     string
	CoreKey         string
	ArtifactVersion int64
	DiscCount       int
}

func (service *Service) MultiDiscTelemetryDimensions(
	ctx context.Context,
	launchID, capability string,
) (MultiDiscTelemetryDimensions, error) {
	var credentialHash []byte
	var state, platformKey, coreKey, contentFormat string
	var hardExpires, artifactVersion int64
	var discCount int
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,
launch.state,
launch.hard_expires_at_ms,
platform.id,
artifact.core_id,
artifact.version,
content.format_version,
(SELECT count(*) FROM launch_external_files file
 WHERE file.launch_session_id=launch.id AND file.kind='DISC')
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN games game ON game.id=launch.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN launch_content_files content ON content.launch_session_id=launch.id
WHERE launch.id=?
`, launchID).Scan(
		&credentialHash, &state, &hardExpires, &platformKey, &coreKey,
		&artifactVersion, &contentFormat, &discCount,
	)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= service.now().UnixMilli() || state != "ACTIVE" ||
		contentFormat != "RETROM_MULTIDISC_M3U_V1" || discCount < 2 || discCount > 8 {
		return MultiDiscTelemetryDimensions{}, ErrCredential
	}
	return MultiDiscTelemetryDimensions{
		PlatformKey: platformKey, CoreKey: coreKey,
		ArtifactVersion: artifactVersion, DiscCount: discCount,
	}, nil
}

type BundleFile struct {
	LogicalName string
	SHA256      string
}

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) emulatorJSConfig(ctx context.Context, launchID, capability string) (Config, error) {
	var credentialHash []byte
	var state, coreID, coreName, artifactID, emulatorVersion, relativePath, compatibilityJSON string
	var dependencySnapshotJSON, variantRevisionID, revisionCompatibilityCode string
	var gameTitle, platformName string
	var logicalName, contentFormat, contentDigest, returnTo string
	var bootstrapExpires, hardExpires, emulatorGameID, initialDiscIndex int64
	var requiresThreads int
	var saveStateID, dosEntry sql.NullString
	var idleExpires sql.NullInt64
	var netplaySessionID, netplayRoomID, netplayProfileJSON sql.NullString
	var netplayPlayerNo sql.NullInt64
	var saveAccess string
	err := service.database.QueryRowContext(ctx, `
SELECT l.credential_sha256,
l.state,
l.bootstrap_expires_at_ms,
l.hard_expires_at_ms,
l.idle_expires_at_ms,
a.core_id,
a.id,
a.runtime_version,
a.entry_path,
a.compatibility_json,
a.requires_threads,
c.name,
r.emulator_game_id,
r.dependency_snapshot_json,
r.compatibility_code,
l.game_variant_revision_id,
metadata.title,
platform.name,
	lc.logical_name,
	lc.format_version,
	content_blob.sha256,
	l.return_to,
l.save_state_id,
l.dos_entry_path,
l.initial_disc_index,
l.netplay_session_id,
l.netplay_player_no,
l.save_access,
session.room_id,
session.profile_json
FROM launch_sessions l
JOIN core_artifacts a ON a.id=l.core_artifact_id
JOIN cores c ON c.id=a.core_id
JOIN game_variant_revisions r ON r.id=l.game_variant_revision_id
JOIN games g ON g.id=l.game_id
JOIN game_metadata_revisions metadata ON metadata.id=g.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=g.platform_instance_id
	JOIN platforms platform ON platform.id=instance.platform_id
	JOIN launch_content_files lc ON lc.launch_session_id=l.id
	JOIN blobs content_blob ON content_blob.id=lc.blob_id
LEFT JOIN netplay_sessions session ON session.id=l.netplay_session_id
WHERE l.id=?
AND l.purpose='PRODUCT'
AND a.runtime_family='EMULATORJS'
AND a.available_for_launch=1
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
			&coreName,
			&emulatorGameID,
			&dependencySnapshotJSON,
			&revisionCompatibilityCode,
			&variantRevisionID,
			&gameTitle,
			&platformName,
			&logicalName,
			&contentFormat,
			&contentDigest,
			&returnTo,
			&saveStateID,
			&dosEntry,
			&initialDiscIndex,
			&netplaySessionID,
			&netplayPlayerNo,
			&saveAccess,
			&netplayRoomID,
			&netplayProfileJSON,
		)
	now := service.now().UnixMilli()
	isNetplay, accessErr := validateConfigAccess(
		err, capability, credentialHash, state, bootstrapExpires, hardExpires, idleExpires,
		saveStateID, dosEntry, netplaySessionID, netplayRoomID, netplayProfileJSON,
		netplayPlayerNo, saveAccess, now,
	)
	if accessErr != nil {
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
	var compatibility artifactCompatibility
	decoder := json.NewDecoder(strings.NewReader(compatibilityJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&compatibility); err != nil || !validArtifactCompatibility(compatibility) {
		return Config{}, ErrCredential
	}
	return service.buildLaunchConfig(ctx, configBuildInput{
		launchID: launchID, capability: capability, coreID: coreID, coreName: coreName, artifactID: artifactID,
		emulatorVersion: emulatorVersion, relativePath: relativePath,
		dependencySnapshotJSON: dependencySnapshotJSON, variantRevisionID: variantRevisionID,
		revisionCompatibilityCode: revisionCompatibilityCode, gameTitle: gameTitle,
		platformName: platformName, logicalName: logicalName, contentFormat: contentFormat,
		contentDigest: contentDigest,
		returnTo:      returnTo, emulatorGameID: emulatorGameID, initialDiscIndex: initialDiscIndex,
		requiresThreads: requiresThreads, saveStateID: saveStateID, dosEntry: dosEntry,
		netplaySessionID: netplaySessionID, netplayRoomID: netplayRoomID,
		netplayProfileJSON: netplayProfileJSON, netplayPlayerNo: netplayPlayerNo,
		isNetplay: isNetplay,
	}, compatibility, version)
}

func validateConfigAccess(
	queryErr error,
	capability string,
	credentialHash []byte,
	state string,
	bootstrapExpires, hardExpires int64,
	idleExpires sql.NullInt64,
	saveStateID, dosEntry, netplaySessionID, netplayRoomID, netplayProfileJSON sql.NullString,
	netplayPlayerNo sql.NullInt64,
	saveAccess string,
	now int64,
) (bool, error) {
	if queryErr != nil || !retromruntime.MatchesCapability(capability, credentialHash) {
		return false, ErrCredential
	}
	isNetplay := netplaySessionID.Valid
	if !validConfigNetplayFields(
		isNetplay, saveStateID, dosEntry, netplayRoomID, netplayProfileJSON, netplayPlayerNo, saveAccess,
	) || !validConfigLifetime(state, bootstrapExpires, hardExpires, idleExpires, now) {
		return false, ErrCredential
	}
	return isNetplay, nil
}

func validConfigNetplayFields(
	isNetplay bool,
	saveStateID, dosEntry, netplayRoomID, netplayProfileJSON sql.NullString,
	netplayPlayerNo sql.NullInt64,
	saveAccess string,
) bool {
	validNetplayFields := netplayPlayerNo.Valid && netplayRoomID.Valid &&
		netplayProfileJSON.Valid && saveAccess == "NETPLAY_DISABLED"
	return isNetplay == validNetplayFields &&
		(isNetplay || saveAccess == "NORMAL") &&
		(!isNetplay || !saveStateID.Valid && !dosEntry.Valid && netplayPlayerNo.Int64 >= 1 && netplayPlayerNo.Int64 <= 4)
}

func validConfigLifetime(
	state string,
	bootstrapExpires, hardExpires int64,
	idleExpires sql.NullInt64,
	now int64,
) bool {
	return hardExpires > now &&
		(state != "CREATED" || bootstrapExpires > now) &&
		(!idleExpires.Valid || idleExpires.Int64 > now) &&
		state != "FINISHED" && state != "EXPIRED" && state != "REVOKED"
}

type configBuildInput struct {
	launchID, capability, coreID, coreName, artifactID, emulatorVersion, relativePath string
	dependencySnapshotJSON, variantRevisionID, revisionCompatibilityCode              string
	gameTitle, platformName, logicalName, contentFormat, contentDigest, returnTo      string
	emulatorGameID, initialDiscIndex                                                  int64
	requiresThreads                                                                   int
	saveStateID, dosEntry                                                             sql.NullString
	netplaySessionID, netplayRoomID, netplayProfileJSON                               sql.NullString
	netplayPlayerNo                                                                   sql.NullInt64
	isNetplay                                                                         bool
}

func (service *Service) buildLaunchConfig(
	ctx context.Context,
	input configBuildInput,
	compatibility artifactCompatibility,
	version *dependencies.Version,
) (Config, error) {
	launchID, coreID, coreName := input.launchID, input.coreID, input.coreName
	artifactID, emulatorVersion, relativePath := input.artifactID, input.emulatorVersion, input.relativePath
	dependencySnapshotJSON := input.dependencySnapshotJSON
	revisionCompatibilityCode := input.revisionCompatibilityCode
	gameTitle, platformName := input.gameTitle, input.platformName
	logicalName, contentFormat, returnTo := input.logicalName, input.contentFormat, input.returnTo
	emulatorGameID, initialDiscIndex := input.emulatorGameID, input.initialDiscIndex
	requiresThreads, saveStateID, dosEntry := input.requiresThreads, input.saveStateID, input.dosEntry
	isNetplay := input.isNetplay
	capability := input.capability
	base := "/runtime/emulatorjs/" + emulatorVersion + "/"
	overrides := map[string]string{compatibility.RequestedArtifactBasename: base + relativePath}
	stateURL := any(nil)
	if saveStateID.Valid && !isNetplay {
		stateURL = "/runtime/launches/" + launchID + "/state"
	}
	biosURL, parentURL, err := service.launchDependencyURLs(ctx, launchID, capability)
	if err != nil {
		return Config{}, err
	}
	coreOptions, warnings, err := buildLaunchCoreOptions(
		compatibility, revisionCompatibilityCode, dependencySnapshotJSON,
	)
	if err != nil {
		return Config{}, err
	}
	gameIdentity, err := ContentIdentity(ContentView{
		Digest: input.contentDigest, Format: contentFormat, CoreID: coreID, DOSEntry: nullableStringPointer(dosEntry),
	})
	if err != nil {
		return Config{}, err
	}
	gameURL, err := RuntimeContentURL("game", gameIdentity, logicalName)
	if err != nil {
		return Config{}, err
	}
	externalFiles, discEntries, err := service.loadLaunchExternalFiles(ctx, launchID)
	if err != nil {
		return Config{}, err
	}
	if !configureDOSLaunch(coreID, contentFormat, dosEntry, externalFiles, coreOptions) {
		return Config{}, ErrBlocked
	}
	discResult, err := launchDiscSet(contentFormat, logicalName, discEntries, initialDiscIndex)
	if err != nil {
		return Config{}, err
	}
	startupActions := slices.Clone(compatibility.StartupActions)
	mode, netplayConfig, err := buildNetplayConfig(input)
	if err != nil {
		return Config{}, err
	}
	playerAdapterID := version.Manifest.EmulatorJS.PlayerAdapter.ID
	if isNetplay {
		playerAdapterID = retromruntime.NetplayPlayerAdapterID
	}
	return Config{
		RuntimeFamily:     "EMULATORJS",
		Mode:              mode,
		LaunchID:          launchID,
		EmulatorJSVersion: emulatorVersion,
		PlayerAdapterID:   playerAdapterID,
		Core:              coreID,
		RuntimeCore:       compatibility.RuntimeCoreID,
		CoreName:          coreName,
		CoreArtifactID:    artifactID,
		EmulatorGameID:    emulatorGameID,
		GameName:          fmt.Sprintf("retrom-%d", emulatorGameID),
		GameTitle:         gameTitle,
		PlatformName:      platformName,
		RuntimeBaseURL: base + strings.TrimSuffix(
			version.Manifest.EmulatorJS.PlayerAdapter.RuntimeBasePath,
			"/",
		) + "/",
		LoaderURL:            base + version.Manifest.EmulatorJS.PlayerAdapter.LoaderPath,
		GameURL:              gameURL,
		BIOSURL:              biosURL,
		ParentURL:            parentURL,
		StateURL:             stateURL,
		InputMode:            compatibility.InputMode,
		StartupActions:       startupActions,
		RequiresThreads:      requiresThreads == 1,
		RuntimePathOverrides: overrides,
		DefaultCoreOptions:   coreOptions,
		ExternalFiles:        externalFiles,
		DiscSet:              discResult.value,
		DOSEntry:             nullableString(dosEntry),
		Warnings:             warnings,
		ReturnTo:             returnTo,
		Netplay:              netplayConfig,
	}, nil
}

func (service *Service) launchDependencyURLs(
	ctx context.Context,
	launchID, capability string,
) (any, any, error) {
	biosURL, err := service.launchBundleURL(ctx, launchID, capability, "BIOS_BUNDLE", "bios")
	if err != nil {
		return nil, nil, err
	}
	parentURL, err := service.launchBundleURL(ctx, launchID, capability, "PARENT", "parent")
	if err != nil {
		return nil, nil, err
	}
	return optionalRuntimeURL(biosURL), optionalRuntimeURL(parentURL), nil
}

func (service *Service) launchBundleURL(
	ctx context.Context,
	launchID, capability, role, kind string,
) (string, error) {
	files, err := service.BundleFiles(ctx, launchID, capability, role)
	if err != nil || len(files) == 0 {
		return "", err
	}
	identity, err := BundleIdentity(files)
	if err != nil {
		return "", err
	}
	return RuntimeContentURL(kind, identity, "bundle.zip")
}

func optionalRuntimeURL(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func buildLaunchCoreOptions(
	compatibility artifactCompatibility,
	revisionCompatibilityCode, dependencySnapshotJSON string,
) (map[string]string, []string, error) {
	coreOptions := make(map[string]string, len(compatibility.DefaultOptions)+4)
	for name, value := range compatibility.DefaultOptions {
		coreOptions[name] = value
	}
	if existing, ok := coreOptions["webgl2Enabled"]; ok && existing != "enabled" {
		return nil, nil, ErrBlocked
	}
	coreOptions["webgl2Enabled"] = "enabled"
	warnings := make([]string, 0)
	if revisionCompatibilityCode == reviewScreenshotOverrideCode {
		warnings = append(warnings, reviewScreenshotOverrideCode)
	}
	biosDependencies, err := corevalidation.ParseRuntimeBIOSDependencies(dependencySnapshotJSON)
	if err != nil {
		return nil, nil, ErrBlocked
	}
	for _, dependency := range biosDependencies {
		if dependency.BlobID == nil || dependency.InstallationStatus == nil {
			continue
		}
		for name, value := range dependency.ActivationOptions {
			if existing, ok := coreOptions[name]; ok && existing != value {
				return nil, nil, ErrBlocked
			}
			coreOptions[name] = value
		}
		if *dependency.InstallationStatus == "HASH_WARNING" {
			warnings = append(warnings, "BIOS_HASH_WARNING")
		}
	}
	return coreOptions, warnings, nil
}

func (service *Service) loadLaunchExternalFiles(
	ctx context.Context,
	launchID string,
) (map[string]string, []DiscEntry, error) {
	externalFiles := make(map[string]string)
	externalRows, externalErr := service.database.QueryContext(ctx, `
SELECT file.virtual_path,
file.logical_name,
file.kind,
blob.sha256
FROM launch_external_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.launch_session_id=?
ORDER BY CASE file.kind WHEN 'DISC' THEN 0 ELSE 1 END,file.virtual_path
	`, launchID)
	if externalErr != nil {
		return nil, nil, fmt.Errorf("launch/service: %w", externalErr)
	}
	defer func() { cleanup.Error("close", externalRows.Close()) }()
	discEntries := make([]DiscEntry, 0, 8)
	for externalRows.Next() {
		var virtualPath, externalName, kind, digest string
		if err := externalRows.Scan(&virtualPath, &externalName, &kind, &digest); err != nil || len(externalFiles) >= 16 {
			return nil, nil, ErrBlocked
		}
		if _, duplicate := externalFiles[virtualPath]; duplicate {
			return nil, nil, ErrBlocked
		}
		if kind == "DISC" {
			index := len(discEntries)
			expectedName := fmt.Sprintf("disc-%03d.chd", index+1)
			if externalName != expectedName || virtualPath != "/"+expectedName {
				return nil, nil, ErrBlocked
			}
			discEntries = append(discEntries, DiscEntry{
				Index: index, Label: fmt.Sprintf("光盘 %d", index+1), VirtualPath: virtualPath,
			})
		} else if kind != "BIOS" {
			return nil, nil, ErrBlocked
		}
		externalIdentity, identityErr := ExternalContentIdentity(digest)
		if identityErr != nil {
			return nil, nil, identityErr
		}
		externalURL, urlErr := RuntimeContentURL("external", externalIdentity, externalName)
		if urlErr != nil {
			return nil, nil, urlErr
		}
		externalFiles[virtualPath] = externalURL
	}
	if err := externalRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("launch/service: %w", err)
	}
	return externalFiles, discEntries, nil
}

type discSetResult struct{ value *DiscSet }

func launchDiscSet(
	contentFormat, logicalName string,
	discEntries []DiscEntry,
	initialDiscIndex int64,
) (discSetResult, error) {
	if contentFormat == "RETROM_MULTIDISC_M3U_V1" {
		if len(discEntries) < 2 || initialDiscIndex < 0 || initialDiscIndex >= int64(len(discEntries)) ||
			logicalName != "playlist.m3u" {
			return discSetResult{}, ErrBlocked
		}
		return discSetResult{value: &DiscSet{
			ContentKind: corevalidation.MultiDiscContentKind, Count: len(discEntries),
			InitialDiscIndex: int(initialDiscIndex), Entries: discEntries,
		}}, nil
	}
	if len(discEntries) != 0 || initialDiscIndex != 0 {
		return discSetResult{}, ErrBlocked
	}
	return discSetResult{}, nil
}

func buildNetplayConfig(input configBuildInput) (string, *NetplayConfig, error) {
	mode := "single"
	var netplayConfig *NetplayConfig
	if input.isNetplay {
		mode = "netplay"
		var profileObject map[string]any
		if json.Unmarshal([]byte(input.netplayProfileJSON.String), &profileObject) != nil ||
			profileObject["coreArtifactId"] != input.artifactID ||
			profileObject["gameVariantRevisionId"] != input.variantRevisionID {
			return "", nil, ErrCredential
		}
		netplayConfig = &NetplayConfig{
			RoomID: input.netplayRoomID.String, SessionID: input.netplaySessionID.String,
			PlayerNo: int(input.netplayPlayerNo.Int64), NetplayProfile: json.RawMessage(input.netplayProfileJSON.String),
			RuntimeSocketURL: "/runtime/netplay/rooms/" + input.netplayRoomID.String + "/socket",
		}
	}
	return mode, netplayConfig, nil
}

func configureDOSLaunch(
	coreID, contentFormat string,
	dosEntry sql.NullString,
	externalFiles, coreOptions map[string]string,
) bool {
	if coreID != "dosbox_pure" || !dosEntry.Valid {
		return true
	}
	return contentFormat == "RETROM_DOS_DIRECT_ZIP_V1" &&
		externalFiles["/game.conf"] == "" && coreOptions["dosbox_pure_conf"] == ""
}

func (service *Service) BundleFiles(ctx context.Context, launchID, capability, kind string) ([]BundleFile, error) {
	if kind != "BIOS_BUNDLE" && kind != "PARENT" {
		return nil, ErrCredential
	}
	var credentialHash []byte
	var state string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT l.credential_sha256,
l.state,
l.hard_expires_at_ms
FROM launch_sessions l
WHERE l.id=?
`, launchID).
		Scan(&credentialHash, &state, &hardExpires)
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
	slices.SortFunc(
		files,
		func(left, right BundleFile) int { return strings.Compare(left.LogicalName, right.LogicalName) },
	)
	return files, nil
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

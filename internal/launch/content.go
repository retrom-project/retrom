package launch

import (
	"context"
	"database/sql"

	retromruntime "retrom/internal/runtime"
)

func (service *Service) ContentBlob(ctx context.Context, launchID, capability, logicalName string) (string, error) {
	content, err := service.Content(ctx, launchID, capability, logicalName)
	return content.Digest, err
}

type ContentView struct {
	Digest          string
	Format          string
	CoreID          string
	DOSEntry        *string
	PlatformKey     string
	ArtifactVersion int64
	DiscCount       int
}

func (service *Service) Content(ctx context.Context, launchID, capability, logicalName string) (ContentView, error) {
	var credentialHash []byte
	var digest, state, format, coreID, platformKey string
	var dosEntry sql.NullString
	var hardExpires, artifactVersion int64
	var discCount int
	err := service.database.QueryRowContext(ctx, `
SELECT l.credential_sha256,
l.state,
l.hard_expires_at_ms,
b.sha256,
lc.format_version,
a.core_id,
a.version,
platform.id,
l.dos_entry_path,
(SELECT count(*) FROM launch_external_files file
 WHERE file.launch_session_id=l.id AND file.kind='DISC')
FROM launch_sessions l
JOIN launch_content_files lc ON lc.launch_session_id=l.id
JOIN blobs b ON b.id=lc.blob_id
JOIN core_artifacts a ON a.id=l.core_artifact_id
JOIN games game ON game.id=l.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE l.id=?
AND lc.logical_name=?
`, launchID, logicalName).Scan(
		&credentialHash, &state, &hardExpires, &digest, &format, &coreID,
		&artifactVersion, &platformKey, &dosEntry, &discCount,
	)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= service.now().UnixMilli() ||
		state != "ACTIVE" {
		return ContentView{}, ErrCredential
	}
	var selected *string
	if dosEntry.Valid {
		selected = &dosEntry.String
	}
	return ContentView{
		Digest: digest, Format: format, CoreID: coreID, DOSEntry: selected,
		PlatformKey: platformKey, ArtifactVersion: artifactVersion, DiscCount: discCount,
	}, nil
}

type ExternalView struct {
	Digest          string
	Kind            string
	PlatformKey     string
	CoreKey         string
	ArtifactVersion int64
	DiscCount       int
}

func (service *Service) External(
	ctx context.Context,
	launchID, capability, logicalName string,
) (ExternalView, error) {
	var credentialHash []byte
	var digest, state, kind, platformKey, coreKey string
	var hardExpires, artifactVersion int64
	var discCount int
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,
launch.state,
launch.hard_expires_at_ms,
blob.sha256,
file.kind,
platform.id,
artifact.core_id,
artifact.version,
(SELECT count(*) FROM launch_external_files disc
 WHERE disc.launch_session_id=launch.id AND disc.kind='DISC')
FROM launch_sessions launch
JOIN launch_external_files file ON file.launch_session_id=launch.id
JOIN blobs blob ON blob.id=file.blob_id
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN games game ON game.id=launch.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE launch.id=?
AND file.logical_name=?
`, launchID, logicalName).Scan(
		&credentialHash, &state, &hardExpires, &digest, &kind, &platformKey,
		&coreKey, &artifactVersion, &discCount,
	)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= service.now().UnixMilli() || state != "ACTIVE" {
		return ExternalView{}, ErrCredential
	}
	return ExternalView{
		Digest: digest, Kind: kind, PlatformKey: platformKey, CoreKey: coreKey,
		ArtifactVersion: artifactVersion, DiscCount: discCount,
	}, nil
}

func (service *Service) ExternalBlob(ctx context.Context, launchID, capability, logicalName string) (string, error) {
	view, err := service.External(ctx, launchID, capability, logicalName)
	if err != nil {
		return "", ErrCredential
	}
	return view.Digest, nil
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

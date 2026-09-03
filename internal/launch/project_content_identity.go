package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/importing"
	retromruntime "retrom/internal/runtime"
)

const RuntimeProjectContentPrefix = "/runtime/content/project/"

type projectIdentityFile struct {
	logicalName string
	format      string
	digest      string
}

func (service *Service) ProjectContentIdentity(
	ctx context.Context,
	sessionID, capability string,
) (string, error) {
	files, err := service.authorizedLaunchProjectIdentityFiles(ctx, sessionID, capability)
	if err != nil {
		files, err = service.authorizedReviewProjectIdentityFiles(ctx, sessionID, capability)
	}
	if err != nil {
		return "", ErrCredential
	}
	return deriveProjectContentIdentity(files)
}

func (service *Service) ProjectContentRoot(
	ctx context.Context,
	sessionID, capability string,
) (string, error) {
	identity, err := service.ProjectContentIdentity(ctx, sessionID, capability)
	if err != nil {
		return "", err
	}
	return RuntimeProjectContentRoot(identity)
}

func (service *Service) authorizedLaunchProjectIdentityFiles(
	ctx context.Context,
	launchID, capability string,
) ([]projectIdentityFile, error) {
	var credentialHash []byte
	var state string
	var bootstrapExpires, hardExpires int64
	var idleExpires sql.NullInt64
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.bootstrap_expires_at_ms,
 launch.hard_expires_at_ms,launch.idle_expires_at_ms
FROM launch_sessions launch
JOIN runtime_target_bindings binding
 ON binding.provider_id=launch.provider_id AND binding.target_id=launch.target_id
WHERE launch.id=? AND binding.delivery_profile IN (
 'FILE_TREE_PROJECT_V1','SEEKABLE_PROJECT_ARCHIVE_V1','ISOLATED_WEB_PROJECT_V1'
)
 AND launch.purpose IN ('PRODUCT','RPG_RUNTIME_VALIDATION')
`, launchID).Scan(&credentialHash, &state, &bootstrapExpires, &hardExpires, &idleExpires)
	if err != nil || !validConfigLifetime(
		state, bootstrapExpires, hardExpires, idleExpires, service.now().UnixMilli(),
	) ||
		!retromruntime.MatchesCapability(capability, credentialHash) {
		return nil, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT content.logical_name,content.format_version,blob.sha256
FROM launch_content_files content
JOIN blobs blob ON blob.id=content.blob_id
WHERE content.launch_session_id=?
 AND content.format_version IN (
  'RPG_MAKER_PROJECT_V1','ONS_PROJECT_V1','KIRIKIRI_PROJECT_V1','BUTTERSCOTCH_PROJECT_V1',
  'TYRANOSCRIPT_PROJECT_V1'
 )
ORDER BY content.logical_name
`, launchID)
	if err != nil {
		return nil, fmt.Errorf("load launch project identity: %w", err)
	}
	defer func() { cleanup.Error("close launch project identity", rows.Close()) }()
	files, readErr := readProjectIdentityFiles(rows)
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read launch project identity: %w", err)
	}
	return files, readErr
}

func (service *Service) authorizedReviewProjectIdentityFiles(
	ctx context.Context,
	previewID, capability string,
) ([]projectIdentityFile, error) {
	var credentialHash []byte
	var state string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT credential_sha256,state,hard_expires_at_ms
FROM review_preview_sessions
WHERE id=? AND content_kind IN (
 'ONS_PROJECT_V1','KIRIKIRI_PROJECT_V1','BUTTERSCOTCH_PROJECT_V1','TYRANOSCRIPT_PROJECT_V1'
)
`, previewID).Scan(&credentialHash, &state, &hardExpires)
	if err != nil || !reviewPreviewCredential(
		service.now().UnixMilli(), capability, credentialHash, state, hardExpires,
	) {
		return nil, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT logical_name,format,digest FROM (
 SELECT session.content_logical_name AS logical_name,session.content_format AS format,
  blob.sha256 AS digest,0 AS sort_order
 FROM review_preview_sessions session
 JOIN blobs blob ON blob.id=session.content_blob_id
 WHERE session.id=?
 UNION ALL
 SELECT file.logical_name,session.content_format,blob.sha256,file.sort_order+1
 FROM review_preview_files file
 JOIN review_preview_sessions session ON session.id=file.preview_session_id
 JOIN blobs blob ON blob.id=file.blob_id
 WHERE file.preview_session_id=? AND file.role='PROJECT_FILE'
) ORDER BY sort_order,logical_name
`, previewID, previewID)
	if err != nil {
		return nil, fmt.Errorf("load review project identity: %w", err)
	}
	defer func() { cleanup.Error("close review project identity", rows.Close()) }()
	files, readErr := readProjectIdentityFiles(rows)
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read review project identity: %w", err)
	}
	return files, readErr
}

type projectIdentityRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func readProjectIdentityFiles(rows projectIdentityRows) ([]projectIdentityFile, error) {
	files := make([]projectIdentityFile, 0)
	for rows.Next() {
		var file projectIdentityFile
		if err := rows.Scan(&file.logicalName, &file.format, &file.digest); err != nil ||
			len(files) >= maximumONSProjectFiles {
			return nil, ErrCredential
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read project identity: %w", err)
	}
	return files, nil
}

func deriveProjectContentIdentity(files []projectIdentityFile) (string, error) {
	if len(files) == 0 || len(files) > maximumONSProjectFiles {
		return "", ErrBlocked
	}
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(left, right projectIdentityFile) int {
		return strings.Compare(left.logicalName, right.logicalName)
	})
	digest := sha256.New()
	_, _ = digest.Write([]byte("RETROM_RUNTIME_PROJECT_V1\x00"))
	previous := ""
	expectedFormat := ordered[0].format
	seen := make(map[string]struct{}, len(ordered))
	for _, file := range ordered {
		normalized, pathErr := importing.ValidateLogicalPath(file.logicalName)
		folded := importing.ASCIICaseFold(normalized)
		_, duplicate := seen[folded]
		if pathErr != nil || normalized != file.logicalName || previous == file.logicalName || duplicate ||
			file.format != expectedFormat || !validProjectContentFormat(file.format) ||
			!validContentDigest(file.digest) {
			return "", ErrBlocked
		}
		seen[folded] = struct{}{}
		_, _ = fmt.Fprintf(
			digest, "%d\x00%s\x00%d\x00%s\x00%s\x00",
			len(file.logicalName), file.logicalName, len(file.format), file.format, file.digest,
		)
		previous = file.logicalName
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func validProjectContentFormat(value string) bool {
	return value == rpgProjectFormat || value == onsProjectFormat || value == kirikiriProjectFormat ||
		value == butterscotchProjectFormat || value == tyranoScriptProjectFormat
}

func RuntimeProjectContentRoot(identity string) (string, error) {
	if !validContentDigest(identity) {
		return "", ErrBlocked
	}
	return RuntimeProjectContentPrefix + identity + "/", nil
}

package launch

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	retromruntime "retrom/internal/runtime"
)

func (service *Service) ReviewPreviewContent(
	ctx context.Context,
	previewID, capability, logicalName string,
) (ContentView, error) {
	var credentialHash []byte
	var digest, state, format, coreID, providerID, targetID, bundleSHA256, platformKey string
	var dosEntry sql.NullString
	var hardExpires int64
	var discCount int
	err := service.database.QueryRowContext(ctx, `
SELECT preview.credential_sha256,preview.state,preview.hard_expires_at_ms,blob.sha256,
preview.content_format,binding.core_id,preview.provider_id,preview.target_id,
preview.bundle_sha256,platform.id,preview.default_dos_entry,
(SELECT count(*) FROM review_preview_files file WHERE file.preview_session_id=preview.id AND file.role='DISC')
FROM review_preview_sessions preview
JOIN blobs blob ON blob.id=preview.content_blob_id
JOIN runtime_target_bindings binding ON binding.provider_id=preview.provider_id AND binding.target_id=preview.target_id
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE preview.id=? AND preview.content_logical_name=?
`, previewID, logicalName).Scan(&credentialHash, &state, &hardExpires, &digest, &format,
		&coreID, &providerID, &targetID, &bundleSHA256, &platformKey, &dosEntry, &discCount)
	if err != nil || !reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return ContentView{}, ErrCredential
	}
	var selected *string
	if dosEntry.Valid {
		selected = &dosEntry.String
	}
	return ContentView{
		Digest: digest, Format: format, CoreID: coreID, ProviderID: providerID, TargetID: targetID,
		BundleSHA256: bundleSHA256, DOSEntry: selected,
		PlatformKey: platformKey, DiscCount: discCount,
	}, nil
}

func (service *Service) ReviewPreviewExternal(
	ctx context.Context,
	previewID, capability, logicalName string,
) (ExternalView, error) {
	var credentialHash []byte
	var digest, state, role, platformKey, coreKey, providerID, targetID, bundleSHA256 string
	var hardExpires int64
	var discCount int
	err := service.database.QueryRowContext(ctx, `
SELECT preview.credential_sha256,preview.state,preview.hard_expires_at_ms,blob.sha256,file.role,
platform.id,binding.core_id,preview.provider_id,preview.target_id,preview.bundle_sha256,
(SELECT count(*) FROM review_preview_files disc WHERE disc.preview_session_id=preview.id AND disc.role='DISC')
FROM review_preview_sessions preview
JOIN review_preview_files file ON file.preview_session_id=preview.id AND file.role IN ('EXTERNAL_FILE','DISC')
JOIN blobs blob ON blob.id=file.blob_id
JOIN runtime_target_bindings binding ON binding.provider_id=preview.provider_id AND binding.target_id=preview.target_id
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE preview.id=? AND file.logical_name=?
`, previewID, logicalName).Scan(&credentialHash, &state, &hardExpires, &digest, &role,
		&platformKey, &coreKey, &providerID, &targetID, &bundleSHA256, &discCount)
	if err != nil || !reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return ExternalView{}, ErrCredential
	}
	kind := "BIOS"
	if role == "DISC" {
		kind = "DISC"
	}
	return ExternalView{
		Digest: digest, Kind: kind, PlatformKey: platformKey, CoreKey: coreKey,
		ProviderID: providerID, TargetID: targetID, BundleSHA256: bundleSHA256,
		DiscCount: discCount,
	}, nil
}

func (service *Service) ReviewPreviewBundleFiles(
	ctx context.Context,
	previewID, capability, kind string,
) ([]BundleFile, error) {
	if kind != "BIOS_BUNDLE" && kind != "PARENT" {
		return nil, ErrCredential
	}
	var credentialHash []byte
	var state string
	var hardExpires int64
	if err := service.database.QueryRowContext(ctx, `
SELECT credential_sha256,state,hard_expires_at_ms FROM review_preview_sessions WHERE id=?
`, previewID).Scan(&credentialHash, &state, &hardExpires); err != nil ||
		!reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return nil, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.logical_name,blob.sha256 FROM review_preview_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.preview_session_id=? AND file.role=? ORDER BY file.sort_order,file.logical_name
`, previewID, kind)
	if err != nil {
		return nil, fmt.Errorf("review preview bundle: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]BundleFile, 0)
	for rows.Next() {
		var file BundleFile
		if err := rows.Scan(&file.LogicalName, &file.SHA256); err != nil {
			return nil, fmt.Errorf("review preview bundle: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review preview bundle rows: %w", err)
	}
	return files, nil
}

func reviewPreviewCredential(now int64, capability string, hash []byte, state string, hardExpires int64) bool {
	return state == "ACTIVE" && hardExpires > now && retromruntime.MatchesCapability(capability, hash)
}

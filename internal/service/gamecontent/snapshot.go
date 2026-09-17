package gamecontent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	model "retrom/internal/model/gamecontent"

	"retrom/internal/capability/content/contentcapability"

	"github.com/google/uuid"
)

func (service *Service) scheduleFresh(
	ctx context.Context,
	scope model.WriteScope,
	gameID, uploadID, contentMode string,
	expectedVersion, now int64,
) (Scheduled, error) {
	binding, err := scope.Content.Binding(ctx, gameID)
	if err != nil {
		return Scheduled{}, fmt.Errorf("load replacement binding: %w", err)
	}
	if binding.Version != expectedVersion {
		return Scheduled{}, model.ErrInvalid
	}
	instanceID, platformID := binding.InstanceID, binding.PlatformID
	coreID, contentPolicy := binding.CoreID, binding.ContentPolicy
	platformVersion, datID := binding.PlatformVersion, binding.DATID
	if platformID == "rpgmaker" && contentMode != contentcapability.ModeRPGMakerProject ||
		platformID != "rpgmaker" && contentMode == contentcapability.ModeRPGMakerProject {
		return Scheduled{}, model.ErrInvalid
	}
	capabilities := contentcapability.Resolve(
		platformID, true, service.multiDiscImportEnabled, contentPolicy,
	)
	if contentMode == contentcapability.ModeMultiDisc && capabilities.MultiDisc == nil {
		return Scheduled{}, model.ErrInvalid
	}
	upload, err := scope.Content.Upload(ctx, uploadID)
	if err != nil {
		return Scheduled{}, fmt.Errorf("load replacement upload: %w", err)
	}
	if err := ValidateUpload(upload, contentMode, platformID); err != nil {
		return Scheduled{}, err
	}
	execution, err := uuid.NewV7()
	if err != nil {
		return Scheduled{}, fmt.Errorf("create replacement execution identity: %w", err)
	}
	executionID := execution.String()
	targetPolicyDigest := contentPolicy.Digest()
	configInput := fmt.Sprintf(
		"%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s",
		instanceID, platformVersion, binding.ProviderID, binding.TargetID, targetPolicyDigest,
		contentMode, pointerText(datID),
	)
	configDigest := sha256.Sum256([]byte(configInput))
	snapshot := model.JobSnapshot{
		ExecutionID:             executionID,
		GameID:                  gameID,
		GameVersion:             expectedVersion,
		BaseManifestDigest:      binding.ManifestDigest,
		UploadSessionID:         uploadID,
		PlatformID:              platformID,
		PlatformInstanceID:      instanceID,
		PlatformInstanceVersion: platformVersion,
		CoreID:                  coreID,
		ProviderID:              binding.ProviderID,
		TargetID:                binding.TargetID,
		ContentPolicy:           contentPolicy,
		TargetPolicyDigest:      targetPolicyDigest,
		ContentMode:             contentMode,
		DATVersionID:            datID,
		ConfigSnapshotDigest:    hex.EncodeToString(configDigest[:]),
		VariantID:               binding.VariantID,
		RPGGeneration:           binding.RPGGeneration,
		RPGDependencySHA256:     binding.RPGDependencySHA256,
		RPGRequirementsSHA256:   binding.RPGRequirementsSHA256,
	}
	if capabilities.MultiDisc != nil && contentMode == contentcapability.ModeMultiDisc {
		snapshot.MaxDiscs = capabilities.MultiDisc.MaxDiscs
		snapshot.MaxTotalBytes = capabilities.MultiDisc.MaxTotalBytes
	}
	return enqueue(ctx, scope.Jobs, snapshot, now)
}

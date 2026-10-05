package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/core/scummvm"
	"retrom/internal/persistence/contentquery"
	"retrom/internal/profilemodel"

	corevalidation "retrom/internal/core/validation"
	dbapi "retrom/internal/database"
	biorepo "retrom/internal/persistence/corevalidation"
	biosservice "retrom/internal/service/corevalidation"
	service "retrom/internal/service/libraryimport"
)

type ReviewRuntime struct {
	ItemID, SnapshotID, PlatformInstanceID, CoreID, ProviderID, TargetID string
	ContentKind, ManifestDigest, Status, Code, DependencyJSON            string
	DATID, DOSEntry                                                      *string
	BIOS                                                                 []corevalidation.BIOSDependency
	Companions                                                           []service.PreparedValidationFile
}

func ReadReviewRuntime(ctx context.Context, executor dbapi.Executor, itemID string) (ReviewRuntime, error) {
	var result ReviewRuntime
	var profile *string
	err := dbapi.QueryRowContext(ctx, executor, `SELECT content.import_item_id,content.source_snapshot_id,
content.target_platform_instance_id,content.core_id,content.provider_id,content.target_id,
source.content_kind,content.source_manifest_digest,content.status,content.compatibility_code,
content.dependency_snapshot_json,content.dat_version_id,content.default_dos_entry,item.review_profile_json
FROM (`+contentquery.CurrentContentSQL+`) content
JOIN import_items item ON item.id=content.import_item_id
JOIN import_item_source_snapshots source ON source.id=content.source_snapshot_id
WHERE content.import_item_id=?`, itemID).Scan(&result.ItemID, &result.SnapshotID, &result.PlatformInstanceID,
		&result.CoreID, &result.ProviderID, &result.TargetID, &result.ContentKind, &result.ManifestDigest,
		&result.Status, &result.Code, &result.DependencyJSON, &result.DATID, &result.DOSEntry, &profile)
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewRuntime{
			ItemID: itemID, Status: "BLOCKED", Code: "CONTENT_ANALYSIS_UNAVAILABLE", DependencyJSON: "{}",
		}, nil
	}
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("read current review content: %w", err)
	}

	current, err := resolveReviewRuntime(ctx, executor, result, profile)
	if err != nil || current.Code == "CONTENT_ANALYSIS_UNAVAILABLE" {
		return current, err
	}
	return applyReviewInputRejection(ctx, executor, current)
}

func resolveReviewRuntime(ctx context.Context, executor dbapi.Executor,
	result ReviewRuntime, profile *string,
) (ReviewRuntime, error) {
	switch result.ContentKind {
	case "RPG_MAKER_PROJECT":
		return readRPGCurrent(ctx, executor, result.ItemID, result)
	case scummvm.ContentKind:
		return readScummCurrent(result, profile)
	}
	if result.Code == "CONTENT_ANALYSIS_UNAVAILABLE" {
		return result, nil
	}
	if observedArcadeMachine(result.DependencyJSON) != "" {
		return readArcadeRuntime(ctx, executor, result)
	}
	if result.ContentKind != "SINGLE_FILE" && result.ContentKind != "DOS_BUNDLE" && result.ContentKind != "MULTI_DISC" {
		return result, nil
	}
	return readStaticCurrent(ctx, executor, result)
}

func readRPGCurrent(
	ctx context.Context, executor dbapi.Executor, itemID string, result ReviewRuntime,
) (ReviewRuntime, error) {
	value, err := BindReviewInputs(executor).RPGProfile(ctx, itemID)
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("resolve current review facts: %w", err)
	}
	dependencies, err := service.ResolveRPGReviewDependencies(value)
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("resolve current review facts: %w", err)
	}
	result.Status, result.Code, result.DependencyJSON = dependencies.Status, dependencies.Code, dependencies.SnapshotJSON
	return result, nil
}

func readScummCurrent(result ReviewRuntime, profile *string) (ReviewRuntime, error) {
	if profile == nil {
		return result, nil
	}
	decoded, err := profilemodel.Decode(profilemodel.Review, *profile)
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("resolve current review facts: %w", err)
	}
	snapshot, valid := decoded.(*scummvm.Snapshot)
	if !valid {
		return ReviewRuntime{}, service.ErrInvalid
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("resolve current review facts: %w", err)
	}
	result.DependencyJSON = string(encoded)
	result.Status, result.Code = snapshot.Status()
	return result, nil
}

func readStaticCurrent(ctx context.Context, executor dbapi.Executor, result ReviewRuntime) (ReviewRuntime, error) {
	name, err := BindReviewInputs(executor).ContentLogicalName(ctx, result.SnapshotID)
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("resolve current review facts: %w", err)
	}
	snapshot, status, code, err := biosservice.New(biorepo.New(executor)).ResolveBIOS(
		ctx, result.ProviderID, result.TargetID, name,
	)
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("resolve current review facts: %w", err)
	}
	if old, parseErr := corevalidation.ParseSnapshot(result.DependencyJSON); parseErr == nil {
		snapshot.MultiDisc = old.MultiDisc
		snapshot.ContentFacts = old.ContentFacts
		if old.ContentRejection != nil {
			result.Status, result.Code = status, code
		}
	}
	if result.Status == "READY" || result.Code == "LAUNCH_BIOS_MISSING" || result.Code == "CONTENT_FILE_BYTES_EXCEEDED" {
		result.Status, result.Code = status, code
	}
	encoded, err := snapshot.JSON()
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("resolve current review facts: %w", err)
	}
	result.DependencyJSON = string(encoded)
	result.BIOS = snapshot.BIOS
	return result, nil
}

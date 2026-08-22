package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/corevalidation"
	"retrom/internal/metadatascrape"
	"retrom/internal/tagging"
)

type creationRun struct {
	service         *Service
	ctx             context.Context
	transaction     *sql.Tx
	plan            creationPlan
	reconfiguration *reconfigurationInput
	importID        string
	jobID           string
	now             int64
	actorUserID     string
	tagReferences   []tagging.Reference
	progress        initialImportProgress
	resultState     string
	rejected        int
	completedAt     any
	scheduledRuns   []metadatascrape.Scheduled
	materialized    map[string]string
	sourceCounts    map[string]int
	duplicateCounts map[string]int
	duplicateItems  int
}

func newCreationRun(
	ctx context.Context,
	service *Service,
	transaction *sql.Tx,
	plan creationPlan,
	reconfiguration *reconfigurationInput,
) *creationRun {
	return &creationRun{
		service: service, ctx: ctx, transaction: transaction, plan: plan, reconfiguration: reconfiguration,
		materialized: make(map[string]string, len(plan.archives)),
		sourceCounts: make(map[string]int), duplicateCounts: make(map[string]int),
	}
}

func (run *creationRun) execute() error {
	defer cleanup.Rollback(run.transaction)
	if err := run.initialize(); err != nil {
		return err
	}
	if err := run.persistArchives(); err != nil {
		return err
	}
	for index := range run.plan.groups {
		if err := run.persistGroup(&run.plan.groups[index]); err != nil {
			return err
		}
	}
	if err := run.finalize(); err != nil {
		return err
	}
	if err := run.transaction.Commit(); err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	run.startScheduledScrapes(run.ctx)
	return nil
}

func (run *creationRun) result() Created {
	return Created{
		ImportJobID: run.importID,
		JobID:       run.jobID,
		State:       run.resultState,
		ItemCount:   len(run.plan.groups),
	}
}

func (run *creationRun) initialize() error {
	if err := run.lockInputs(); err != nil {
		return err
	}
	actor := reviewActor(run.ctx)
	actorUserID, actorIsUser := actor.UserID.(string)
	if len(run.plan.request.TagIDs) > 0 && (!actorIsUser || actorUserID == "") {
		return ErrInvalid
	}
	run.actorUserID = actorUserID
	tagReferences, err := run.service.tags.ValidateReferences(
		run.ctx, run.transaction, run.plan.request.TagIDs,
	)
	if err != nil {
		return fmt.Errorf("libraryimport/service: validate import tags: %w", err)
	}
	run.tagReferences = tagReferences
	biosCatalog, err := corevalidation.Catalog(run.ctx, run.transaction, run.plan.target.artifactID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	if err := prepareStaticBIOSDependencies(
		run.ctx, run.transaction, run.plan.target.artifactID, run.plan.target.platformID, run.plan.groups,
	); err != nil {
		return err
	}
	importID, _ := uuid.NewV7()
	jobID, _ := uuid.NewV7()
	run.importID, run.jobID = importID.String(), jobID.String()
	run.now = run.service.now().UnixMilli()
	configJSON, configDigest := run.configSnapshot(biosCatalog)
	if err := run.insertJob(configJSON, configDigest); err != nil {
		return err
	}
	return run.insertImport(configJSON, configDigest)
}

func (run *creationRun) lockInputs() error {
	var uploadState string
	if err := run.transaction.QueryRowContext(run.ctx, `
SELECT state FROM upload_sessions WHERE id=?
`, run.plan.request.UploadID).Scan(&uploadState); err != nil || uploadState != "COMPLETE" {
		return ErrInvalid
	}
	var version, artifactVersion int64
	var artifactID, compatibilityConfig string
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT pi.version,a.id,a.version,a.compatibility_config_json
FROM platform_instances pi
JOIN core_artifacts a ON a.core_id=pi.default_core_id AND a.enabled=1
WHERE pi.id=? AND pi.enabled=1 AND pi.deleted_at_ms IS NULL
`, run.plan.request.TargetPlatformInstanceID).Scan(
		&version, &artifactID, &artifactVersion, &compatibilityConfig,
	)
	target := run.plan.target
	if err != nil || version != target.instanceVersion || artifactID != target.artifactID ||
		artifactVersion != target.artifactVersion ||
		compatibilityConfigDigest(compatibilityConfig) != compatibilityConfigDigest(target.compatibilityConfig) {
		return ErrInvalid
	}
	return nil
}

func (run *creationRun) configSnapshot(biosCatalog []corevalidation.BIOSCatalogEntry) ([]byte, string) {
	target := run.plan.target
	config := map[string]any{
		"schemaVersion": 2, "contentMode": run.plan.contentMode,
		"platformInstanceId":      run.plan.request.TargetPlatformInstanceID,
		"platformInstanceVersion": target.instanceVersion, "platformId": target.platformID,
		"defaultCoreId": target.coreID, "coreArtifactId": target.artifactID,
		"emulatorjsVersion": target.emulatorVersion, "coreArtifactPath": target.artifactPath,
		"coreArtifactSha256": target.artifactSHA, "coreArtifactVersion": target.artifactVersion,
		"compatibilityConfigDigest": compatibilityConfigDigest(target.compatibilityConfig),
		"datVersionId":              nullable(run.plan.datID), "biosRequirements": biosCatalog,
		"metadataProviderConfigVersion": 1, "tags": run.tagReferences,
	}
	if run.plan.contentMode == contentcapability.ModeMultiDiscM3UV1 {
		capabilities := contentcapability.Resolve(
			target.platformID, true, run.service.multiDiscImportEnabled, target.compatibilityConfig,
		)
		config["multiDisc"] = capabilities.MultiDisc
	}
	configJSON, _ := json.Marshal(config)
	digest := sha256.Sum256(configJSON)
	return configJSON, hex.EncodeToString(digest[:])
}

func (run *creationRun) startScheduledScrapes(ctx context.Context) {
	for _, scheduled := range run.scheduledRuns {
		if scheduled.IsNoop() {
			continue
		}
		runID := scheduled.ScrapeRunID()
		go func() { _ = run.service.scraper.Run(context.WithoutCancel(ctx), runID) }()
	}
}

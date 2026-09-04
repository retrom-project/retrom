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
	queued          *queuedCreationWork
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

func newQueuedCreationRun(
	ctx context.Context,
	service *Service,
	transaction *sql.Tx,
	plan creationPlan,
	work queuedCreationWork,
) *creationRun {
	run := newCreationRun(ctx, service, transaction, plan, nil)
	run.queued = &work
	run.importID, run.jobID = work.importID, work.jobID
	return run
}

func (run *creationRun) execute() error {
	defer cleanup.Rollback(run.transaction)
	if err := run.initialize(); err != nil {
		return fmt.Errorf("libraryimport/service: initialize import: %w", err)
	}
	if err := run.persistArchives(); err != nil {
		return fmt.Errorf("libraryimport/service: persist import archives: %w", err)
	}
	for index := range run.plan.groups {
		if err := run.persistGroup(&run.plan.groups[index]); err != nil {
			return fmt.Errorf("libraryimport/service: persist import group %d: %w", index, err)
		}
	}
	if err := run.finalize(); err != nil {
		return fmt.Errorf("libraryimport/service: finalize import: %w", err)
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
		return fmt.Errorf("lock import inputs: %w", err)
	}
	if run.queued != nil && !run.queued.accepts(run.plan.target) {
		return ErrInvalid
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
	biosCatalog, err := corevalidation.Catalog(
		run.ctx, run.transaction, run.plan.target.providerID, run.plan.target.targetID,
	)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	if err := prepareStaticBIOSDependencies(
		run.ctx, run.transaction, run.plan.target.providerID, run.plan.target.targetID,
		run.plan.target.platformID, run.plan.groups,
	); err != nil {
		return fmt.Errorf("prepare import BIOS dependencies: %w", err)
	}
	if run.queued == nil {
		importID, _ := uuid.NewV7()
		jobID, _ := uuid.NewV7()
		run.importID, run.jobID = importID.String(), jobID.String()
	}
	run.now = run.service.now().UnixMilli()
	configJSON, configDigest := run.configSnapshot(biosCatalog)
	if run.queued != nil {
		if err := run.updateQueuedImport(configJSON, configDigest); err != nil {
			return fmt.Errorf("update queued import: %w", err)
		}
		return nil
	}
	if err := run.insertJob(configJSON, configDigest); err != nil {
		return fmt.Errorf("insert import job: %w", err)
	}
	if err := run.insertImport(configJSON, configDigest); err != nil {
		return fmt.Errorf("insert import: %w", err)
	}
	return nil
}

func (run *creationRun) lockInputs() error {
	var uploadState string
	if err := run.transaction.QueryRowContext(run.ctx, `
SELECT state FROM upload_sessions WHERE id=?
`, run.plan.request.UploadID).Scan(&uploadState); err != nil || uploadState != "COMPLETE" {
		return ErrInvalid
	}
	var version int64
	var providerID, targetID, targetContractSHA256 string
	target := run.plan.target
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT pi.version,target.provider_id,target.target_id,target.target_contract_sha256
FROM platform_instances pi
JOIN runtime_target_bindings binding ON binding.core_id=? AND binding.provider_id=? AND binding.target_id=?
 AND binding.launch_policy!='DISABLED'
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=pi.platform_id
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
WHERE pi.id=? AND pi.default_core_id=? AND pi.enabled=1 AND pi.deleted_at_ms IS NULL
`, target.coreID, target.providerID, target.targetID,
		run.plan.request.TargetPlatformInstanceID, target.defaultCoreID).Scan(
		&version, &providerID, &targetID, &targetContractSHA256,
	)
	if err != nil || version != target.instanceVersion || providerID != target.providerID ||
		targetID != target.targetID || targetContractSHA256 != target.targetContractSHA256 {
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
		"defaultCoreId": target.defaultCoreID, "resolvedCoreId": target.coreID,
		"providerId": target.providerID, "targetId": target.targetID,
		"targetContractSha256":  target.targetContractSHA256,
		"gameCompatibilityLine": target.gameCompatibilityLine,
		"contentPolicyDigest":   compatibilityConfigDigest(target.contentPolicyJSON),
		"datVersionId":          nullable(run.plan.datID), "biosRequirements": biosCatalog,
		"metadataProviderConfigVersion": 1, "tags": run.tagReferences,
	}
	if run.plan.contentMode == contentcapability.ModeMultiDisc {
		capabilities := contentcapability.Resolve(
			target.platformID, true, run.service.multiDiscImportEnabled, target.contentPolicyJSON,
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

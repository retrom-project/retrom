package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	application "retrom/internal/model/libraryimport"
	taggingmodel "retrom/internal/model/tagging"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/content/multidisc"
	"retrom/internal/capability/engine/rpgmaker/detector"
	metadatascrapemodel "retrom/internal/model/metadatascrape"
	"retrom/internal/repo/dbexec"
	metadatapersistence "retrom/internal/repo/metadatascrape"
	tagpersistence "retrom/internal/repo/tagging"
)

type ReviewDetail struct{ database *sql.DB }

func NewReviewDetail(database *sql.DB) *ReviewDetail { return &ReviewDetail{database: database} }

func (repository *ReviewDetail) LoadReviewDetail(ctx context.Context, itemID string) (application.ReviewDetail, error) {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ReviewDetail{}, fmt.Errorf("begin review detail snapshot: %w", err)
	}
	defer dbexec.Rollback(transaction)

	drafts := ReviewDrafts{transaction}
	media := ReviewMedia{transaction}
	sources := ReviewSources{transaction}
	validation := BindReviewValidation(transaction)
	duplicates := BindContentDuplicates(transaction)
	dependencies := BindReviewDependencies(transaction)
	metadata := metadatapersistence.BindEvidenceQueries(transaction)
	tags := tagpersistence.BindCrossDomain(transaction)

	head, err := drafts.Head(ctx, itemID)
	if err != nil {
		return application.ReviewDetail{}, fmt.Errorf("read review headline: %w", err)
	}

	result, err := projectHead(head)
	if err != nil {
		return application.ReviewDetail{}, err
	}

	evidence, err := loadReviewEvidence(ctx, metadata, head.ItemID)
	if err != nil {
		return application.ReviewDetail{}, fmt.Errorf("read metadata evidence: %w", err)
	}
	result.Candidates, result.ScrapeRuns = evidence.Candidates, evidence.Runs

	if err := assembleMedia(ctx, media, sources, head, &result); err != nil {
		return application.ReviewDetail{}, err
	}

	result.SelectedAssets.ScreenshotIDs, err = drafts.ScreenshotIDs(ctx, head.ItemID)
	if err != nil {
		return application.ReviewDetail{}, fmt.Errorf("read review screenshot selection: %w", err)
	}
	result.DOSEntries, err = drafts.DOSEntries(ctx, head.ItemID)
	if err != nil {
		return application.ReviewDetail{}, fmt.Errorf("read review DOS entries: %w", err)
	}

	result.DuplicateGames, result.ContentIdentityDigest, err = inspectDuplicates(ctx, duplicates.executor,
		application.ContentSnapshot{ID: head.SnapshotID, Kind: head.ContentKind}, head.PlatformID)
	if err != nil {
		return application.ReviewDetail{}, fmt.Errorf("inspect content duplicates: %w", err)
	}
	if err := assembleDependencies(ctx, dependencies, head, &result); err != nil {
		return application.ReviewDetail{}, err
	}

	if err := assembleValidation(ctx, validation, media, head, &result); err != nil {
		return application.ReviewDetail{}, err
	}

	refs, err := tags.ReadOwnerReferences(ctx, taggingmodel.Owner{Kind: taggingmodel.OwnerReviewDraft, ID: head.DraftID})
	if err != nil {
		return application.ReviewDetail{}, fmt.Errorf("read review tags: %w", err)
	}
	result.Tags = refs
	if result.Tags == nil {
		result.Tags = []taggingmodel.Reference{}
	}

	if err := transaction.Commit(); err != nil {
		return application.ReviewDetail{}, fmt.Errorf("finish review detail snapshot: %w", err)
	}
	return result, nil
}

func projectHead(head application.ReviewHead) (application.ReviewDetail, error) {
	metadata, err := reviewDocument(head.MetadataJSON)
	if err != nil {
		return application.ReviewDetail{}, err
	}
	manifest, err := reviewDocument(head.SourceManifestJSON)
	if err != nil {
		return application.ReviewDetail{}, err
	}
	if len(manifest) > 0 && manifest[0] == '[' {
		manifest, err = json.Marshal(struct {
			Files json.RawMessage `json:"files"`
		}{manifest})
		if err != nil {
			return application.ReviewDetail{}, fmt.Errorf("encode review source manifest: %w", err)
		}
	}
	return application.ReviewDetail{
		ItemID: head.ItemID, ImportJobID: head.ImportJobID, Version: head.Version, UpdatedAtMS: head.UpdatedAtMS,
		SnapshotID: head.SnapshotID, PlatformInstance: head.PlatformInstance, Metadata: metadata, SourceManifest: manifest,
		SelectedCandidateID: head.SelectedCandidateID, DefaultDOSEntry: head.DefaultDOSEntry,
		SelectedAssets: application.ReviewSelectedAssets{
			CoverID: head.CoverID, UploadedCoverID: head.UploadedCoverID, BackgroundID: head.BackgroundID,
		},
	}, nil
}

func reviewDocument(value string) (json.RawMessage, error) {
	var result json.RawMessage
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil, fmt.Errorf("decode review document: %w", err)
	}
	return result, nil
}

func loadReviewEvidence(
	ctx context.Context,
	reader metadatascrapemodel.ReviewEvidenceReader,
	itemID string,
) (metadatascrapemodel.ReviewEvidence, error) {
	records, err := reader.ReviewCandidates(ctx, itemID)
	if err != nil {
		return metadatascrapemodel.ReviewEvidence{}, fmt.Errorf("read review candidates: %w", err)
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	var assets []metadatascrapemodel.CandidateAssetView
	if len(ids) > 0 {
		assets, err = reader.CandidateAssets(ctx, ids)
		if err != nil {
			return metadatascrapemodel.ReviewEvidence{}, fmt.Errorf("read candidate assets: %w", err)
		}
	}
	if assets == nil {
		assets = []metadatascrapemodel.CandidateAssetView{}
	}
	byCandidate := make(map[string][]metadatascrapemodel.CandidateAssetView, len(ids))
	for _, asset := range assets {
		byCandidate[asset.CandidateID] = append(byCandidate[asset.CandidateID], asset)
	}
	result := metadatascrapemodel.ReviewEvidence{Candidates: make([]metadatascrapemodel.ReviewCandidate, 0, len(records))}
	for _, record := range records {
		metadata, err := decodeReviewCandidateDocument(record.MetadataJSON)
		if err != nil {
			return metadatascrapemodel.ReviewEvidence{}, err
		}
		evidence, err := decodeReviewCandidateDocument(record.EvidenceJSON)
		if err != nil {
			return metadatascrapemodel.ReviewEvidence{}, err
		}
		selected := byCandidate[record.ID]
		if selected == nil {
			selected = []metadatascrapemodel.CandidateAssetView{}
		}
		result.Candidates = append(result.Candidates, metadatascrapemodel.ReviewCandidate{
			ID: record.ID, RunID: record.RunID, ProviderGameID: record.ProviderGameID,
			Metadata: metadata, Evidence: evidence, Assets: selected, CreatedAtMS: record.CreatedAtMS,
		})
	}
	result.Runs, err = reader.ReviewRuns(ctx, itemID)
	if err != nil {
		return metadatascrapemodel.ReviewEvidence{}, fmt.Errorf("read review scrape runs: %w", err)
	}
	if result.Runs == nil {
		result.Runs = []metadatascrapemodel.ReviewRun{}
	}
	return result, nil
}

func decodeReviewCandidateDocument(raw string) (json.RawMessage, error) {
	var result json.RawMessage
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("decode review candidate document: %w", err)
	}
	return result, nil
}

func assembleMedia(
	ctx context.Context,
	media application.ReviewMediaReader,
	sources application.ReviewSourceReader,
	head application.ReviewHead,
	result *application.ReviewDetail,
) error {
	var err error
	result.UploadedAssets, err = media.UploadedAssets(ctx, head.ItemID)
	if err != nil {
		return fmt.Errorf("read uploaded review assets: %w", err)
	}
	sourceMedia, found, err := media.SourceMedia(ctx, head.ItemID)
	if err != nil {
		return fmt.Errorf("read review source media: %w", err)
	}
	if found {
		result.SourceMedia = &sourceMedia
		if result.SourceMedia.Label != nil && *result.SourceMedia.Label == "" {
			result.SourceMedia.Label = nil
		}
		if !result.SourceMedia.HasCover {
			result.SourceMedia.CoverWidthPX = nil
			result.SourceMedia.CoverHeightPX = nil
		}
	}
	result.SourceFiles, err = readSourceFiles(ctx, sources, head.SnapshotID, head.ContentKind)
	return err
}

func readSourceFiles(
	ctx context.Context,
	reader application.ReviewSourceReader,
	snapshotID, contentKind string,
) ([]application.ReviewSourceFile, error) {
	records, err := reader.Files(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("read review source files: %w", err)
	}
	files := make([]application.ReviewSourceFile, 0, len(records))
	for _, record := range records {
		file := application.ReviewSourceFile{
			ID: record.ID, Name: record.Name, SizeBytes: record.SizeBytes, SHA256: record.SHA256, MD5: record.MD5,
			CRC32: record.CRC32, Archive: record.Archive, ArchiveEntries: []application.ReviewArchiveEntry{},
		}
		if record.ArchiveBlobID != nil {
			archive, err := reader.ArchiveEntries(ctx, *record.ArchiveBlobID)
			if err != nil {
				return nil, fmt.Errorf("read review archive entries: %w", err)
			}
			file.ArchiveEntries = archive.Entries
			file.ArchiveFormat = projectArchiveFormat(contentKind, record.Name, archive.Format)
		}
		files = append(files, file)
	}
	return files, nil
}

func projectArchiveFormat(contentKind, name string, format *string) *string {
	if contentKind == "TYRANOSCRIPT_PROJECT" && format != nil && *format == "ZIP" &&
		strings.EqualFold(path.Ext(name), ".exe") {
		value := "NWJS_EXECUTABLE"
		return &value
	}
	return format
}

func assembleDependencies(
	ctx context.Context,
	reader application.ReviewDependencyReader,
	head application.ReviewHead,
	result *application.ReviewDetail,
) error {
	dependencyHead := application.ReviewDependencyHead{
		SnapshotID: head.SnapshotID, ContentKind: head.ContentKind, PlatformID: head.PlatformID,
		ValidationStatus: head.ValidationStatus, CompatibilityCode: head.CompatibilityCode,
		DependencyJSON: head.DependencyJSON,
	}
	if head.PlatformID == "arcade" && head.DependencyJSON != nil {
		arcade, found, err := assembleArcade(ctx, reader, head.ItemID, dependencyHead)
		if err != nil {
			return err
		}
		if found {
			result.ArcadeDependencies = &arcade
		}
	}
	if head.ContentKind == multidisc.ContentKind {
		discs, found, err := assembleMultiDisc(ctx, reader, head.ItemID, dependencyHead)
		if err != nil {
			return err
		}
		if found {
			result.MultiDisc = &discs
		}
	}
	return nil
}

type arcadeDraftDependency struct {
	Kind                string   `json:"kind"`
	Machine             string   `json:"machine"`
	RequiredBy          *string  `json:"requiredBy,omitempty"`
	Depth               int      `json:"depth,omitempty"`
	ExpectedLogicalName string   `json:"expectedLogicalName,omitempty"`
	State               string   `json:"state"`
	RequiredEntryCount  int      `json:"requiredEntryCount,omitempty"`
	RequiredEntries     []string `json:"requiredEntries"`
}

type arcadeDraftSnapshot struct {
	SchemaVersion     int                     `json:"schemaVersion"`
	Kind              string                  `json:"kind"`
	Machine           string                  `json:"machine"`
	DatVersionID      string                  `json:"datVersionId"`
	Closure           json.RawMessage         `json:"closure"`
	Dependencies      []arcadeDraftDependency `json:"dependencies"`
	MissingEntries    []string                `json:"missingEntries"`
	MismatchedEntries []string                `json:"mismatchedEntries"`
	Warnings          []string                `json:"warnings"`
}

func assembleArcade(
	ctx context.Context,
	reader application.ReviewDependencyReader,
	itemID string,
	head application.ReviewDependencyHead,
) (application.ReviewArcade, bool, error) {
	var snapshot arcadeDraftSnapshot
	if !parseArcadeSnapshot(head.DependencyJSON, &snapshot) {
		return application.ReviewArcade{}, false, application.ErrInvalid
	}
	if err := resolveArcadeClosure(ctx, reader, &snapshot); err != nil {
		return application.ReviewArcade{}, false, err
	}
	sortArcadeDependencies(snapshot.Dependencies)

	attachments, err := reader.ArcadeAttachments(ctx, itemID)
	if err != nil {
		return application.ReviewArcade{}, false, fmt.Errorf(
			"read review arcade attachments: %w", err,
		)
	}
	byMachine, active := indexArcadeAttachments(attachments)
	status := optionalText(head.ValidationStatus)
	code := optionalText(head.CompatibilityCode)
	result := application.ReviewArcade{
		Machine: snapshot.Machine, Status: status,
		CompatibilityCode: code,
		Nodes:             buildArcadeNodes(snapshot.Dependencies, byMachine, active, code),
		ActiveAttachment:  active,
	}
	return result, true, nil
}

func parseArcadeSnapshot(
	dependencyJSON *string, snapshot *arcadeDraftSnapshot,
) bool {
	if dependencyJSON == nil {
		return false
	}
	if json.Unmarshal([]byte(*dependencyJSON), snapshot) != nil {
		return false
	}
	return snapshot.SchemaVersion == corevalidation.SnapshotSchemaVersion &&
		snapshot.Kind == corevalidation.SnapshotKindArcade &&
		snapshot.Machine != "" && snapshot.DatVersionID != "" &&
		snapshot.Dependencies != nil
}

func resolveArcadeClosure(
	ctx context.Context,
	reader application.ReviewDependencyReader,
	snapshot *arcadeDraftSnapshot,
) error {
	cache := make(map[string]application.ArcadeMachineRelation)
	missing := make(map[string]bool)
	var failure error
	resolve := func(name string) (application.ArcadeMachineRelation, bool) {
		if failure != nil || missing[name] {
			return application.ArcadeMachineRelation{}, false
		}
		if rel, exists := cache[name]; exists {
			return rel, true
		}
		rel, found, err := reader.MachineRelation(
			ctx, snapshot.DatVersionID, name,
		)
		if err != nil {
			failure = err
			return application.ArcadeMachineRelation{}, false
		}
		if !found {
			missing[name] = true
			return application.ArcadeMachineRelation{}, false
		}
		cache[name] = rel
		return rel, true
	}
	nodes, cyclic, available := application.ArcadeDependencyClosure(
		snapshot.Machine, resolve,
	)
	if failure != nil {
		return fmt.Errorf(
			"read arcade dependency relation: %w", failure,
		)
	}
	if !available || cyclic {
		return application.ErrInvalid
	}
	byNode := make(map[string]application.ArcadeClosureNode, len(nodes))
	for _, node := range nodes {
		byNode[node.Machine] = node
	}
	for i := range snapshot.Dependencies {
		dep := &snapshot.Dependencies[i]
		node, exists := byNode[dep.Machine]
		if !exists || node.Kind != dep.Kind {
			return application.ErrInvalid
		}
		dep.RequiredBy = node.RequiredBy
		dep.Depth = node.Depth
		dep.ExpectedLogicalName = dep.Machine + ".zip"
		dep.RequiredEntryCount = len(dep.RequiredEntries)
	}
	return nil
}

func sortArcadeDependencies(deps []arcadeDraftDependency) {
	sort.Slice(deps, func(left, right int) bool {
		if deps[left].Kind != deps[right].Kind {
			return deps[left].Kind < deps[right].Kind
		}
		if deps[left].Depth != deps[right].Depth {
			return deps[left].Depth < deps[right].Depth
		}
		return deps[left].Machine < deps[right].Machine
	})
}

func buildArcadeNodes(
	deps []arcadeDraftDependency,
	byMachine map[string]*application.ArcadeAttachment,
	active *application.ArcadeAttachment,
	code string,
) []application.ReviewArcadeNode {
	unsupported := arcadeUnsupported(code)
	nodes := make([]application.ReviewArcadeNode, 0, len(deps))
	for _, dep := range deps {
		node := application.ReviewArcadeNode{
			Kind: dep.Kind, Machine: dep.Machine,
			RequiredBy: dep.RequiredBy, Depth: dep.Depth,
			ExpectedLogicalName: dep.ExpectedLogicalName,
			State:               dep.State, RequiredEntryCount: dep.RequiredEntryCount,
			RequiredEntries: dep.RequiredEntries,
			Attachment:      byMachine[dep.Machine],
		}
		node.CanAttach = dep.Kind == "PARENT" &&
			(dep.State == "MISSING" || dep.State == "MISMATCH") &&
			active == nil && !unsupported
		nodes = append(nodes, node)
	}
	return nodes
}

func assembleMultiDisc(
	ctx context.Context,
	reader application.ReviewDependencyReader,
	itemID string,
	head application.ReviewDependencyHead,
) (application.ReviewMultiDisc, bool, error) {
	source, err := reader.MultiDiscSource(ctx, head.SnapshotID)
	if err != nil {
		return application.ReviewMultiDisc{}, false, fmt.Errorf("read review multi-disc source: %w", err)
	}
	result := application.ReviewMultiDisc{
		ContentKind: multidisc.ContentKind, Playlist: source.Playlist,
		DiscCount: len(source.Entries), MaxDiscs: source.MaxDiscs,
		MaxTotalBytes: source.MaxTotalBytes, Entries: source.Entries, MissingReferences: []string{},
	}
	for i := range result.Entries {
		entry := &result.Entries[i]
		entry.DiscIndex = entry.Index
		entry.Label = fmt.Sprintf("光盘 %d", entry.Index+1)
		if entry.SizeBytes != nil {
			result.TotalPresentBytes += *entry.SizeBytes
			result.PresentDiscCount++
		}
		if entry.State == "MISSING" {
			result.MissingReferences = append(result.MissingReferences, entry.SourceReference)
		}
	}
	result.MissingDiscCount = len(result.MissingReferences)
	attachments, err := reader.MultiDiscAttachments(ctx, itemID)
	if err != nil {
		return application.ReviewMultiDisc{}, false, fmt.Errorf("read review multi-disc attachments: %w", err)
	}
	retryRequired := false
	for i := range attachments {
		a := &attachments[i]
		a.CanRetry = a.Retryable()
		if result.LatestAttachment == nil {
			result.LatestAttachment = a
		}
		if result.ActiveAttachment == nil && a.Active() {
			result.ActiveAttachment = a
		}
		if a.State == "FAILED_RETRYABLE" {
			retryRequired = true
		}
	}
	result.CanAttachMissingDiscs = result.MissingDiscCount > 0 && result.ActiveAttachment == nil && !retryRequired
	return result, true, nil
}

func assembleValidation(
	ctx context.Context,
	validation application.ReviewValidationReader,
	media application.ReviewMediaReader,
	head application.ReviewHead,
	result *application.ReviewDetail,
) error {
	current, err := projectValidation(ctx, validation, head, result)
	if err != nil {
		return err
	}
	if current && head.ValidationID != nil {
		screenshot, found, err := media.RuntimeScreenshot(ctx, head.ItemID, *head.ValidationID)
		if err != nil {
			return fmt.Errorf("read review runtime screenshot: %w", err)
		}
		if found {
			result.RuntimeScreenshot = &screenshot
		}
	}
	if result.MultiDisc != nil && head.ValidationID != nil && !current {
		result.MultiDisc.CanAttachMissingDiscs = false
	}
	result.CanApprove = reviewApproval(head.ContentKind, result.CanApprove, result.RuntimeScreenshot != nil)
	profile, found, err := validation.Profile(ctx, head.DraftID)
	if err != nil {
		return fmt.Errorf("read review RPG profile: %w", err)
	}
	if found {
		result.RPGMaker, err = projectRPGMaker(profile)
	}
	return err
}

func projectValidation(
	ctx context.Context,
	reader application.ReviewValidationReader,
	head application.ReviewHead,
	result *application.ReviewDetail,
) (bool, error) {
	if head.ValidationID == nil {
		return false, nil
	}
	evidence, err := reader.Evidence(ctx, *head.ValidationID)
	if err != nil {
		return false, fmt.Errorf("read review validation evidence: %w", err)
	}
	input, isCurrent := evidence.CurrentInput()
	current := isCurrent && application.PrepublishDigestMatches(
		evidence.InputDigest, input,
	)
	if current && evidence.ContentKind == "RPG_MAKER_PROJECT" {
		current, err = checkRPGDependencyCurrency(
			ctx, reader, evidence,
		)
		if err != nil {
			return false, err
		}
	}
	dependency, err := parseDependencyDocument(head.DependencyJSON)
	if err != nil {
		return false, err
	}
	ready := optionalText(head.ValidationStatus) == "READY"
	result.Validation = &application.ReviewValidationView{
		ID:                 *head.ValidationID,
		Status:             optionalText(head.ValidationStatus),
		Current:            current && ready,
		CompatibilityCode:  optionalText(head.CompatibilityCode),
		DependencySnapshot: dependency,
	}
	result.CanApprove = head.SelectedValidationID != nil &&
		current && ready && head.Policy.Supports(head.ContentKind)
	return current, nil
}

func checkRPGDependencyCurrency(
	ctx context.Context,
	reader application.ReviewValidationReader,
	evidence application.ReviewValidationEvidence,
) (bool, error) {
	profile, found, err := reader.Profile(ctx, evidence.DraftID)
	if err != nil {
		return false, fmt.Errorf("read current RPG profile: %w", err)
	}
	if !found {
		return false, application.ErrInvalid
	}
	deps, err := application.ResolveRPGReviewDependencies(profile)
	if err != nil {
		return false, fmt.Errorf("resolve RPG review dependencies: %w", err)
	}
	return deps.SnapshotJSON == evidence.DependencyJSON &&
		deps.Status == evidence.Status &&
		deps.Code == evidence.CompatibilityCode &&
		deps.Digest == profile.DependencySHA256, nil
}

func parseDependencyDocument(
	dependencyJSON *string,
) (json.RawMessage, error) {
	if dependencyJSON == nil {
		return nil, nil
	}
	return reviewDocument(*dependencyJSON)
}

func projectRPGMaker(profile application.RPGReviewProfile) (*application.ReviewRPGMaker, error) {
	var analysis application.RPGReviewAnalysis
	if err := json.Unmarshal([]byte(profile.AnalysisJSON), &analysis); err != nil {
		return nil, fmt.Errorf("decode review RPG profile: %w", err)
	}
	gen := detector.Generation(profile.Generation)
	dependencies := detector.ExternalRTPRequirements(
		gen, analysis.SelfContained, analysis.Requirements.RTP,
	)
	requirements := make([]application.ReviewRTPDeclaration, 0, len(dependencies))
	for _, entry := range dependencies {
		requirements = append(requirements, application.ReviewRTPDeclaration{
			Slot: int64(entry.Slot), DeclaredName: entry.DeclaredName,
		})
	}
	return &application.ReviewRPGMaker{
		SelectedCoreID: profile.SelectedCoreID, Generation: profile.Generation,
		EvidenceGeneration: profile.EvidenceGeneration, EvidenceConfidence: profile.EvidenceConfidence,
		SelfContained: analysis.SelfContained, SelfContainedOverride: profile.SelfContainedOverride,
		ExternalRTPRequirements: requirements,
	}, nil
}

func reviewApproval(contentKind string, selectedReady, hasCurrentScreenshot bool) bool {
	return selectedReady || contentKind != "SCUMMVM_PROJECT" && hasCurrentScreenshot
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func indexArcadeAttachments(
	attachments []application.ArcadeAttachment,
) (map[string]*application.ArcadeAttachment, *application.ArcadeAttachment) {
	byMachine := make(map[string]*application.ArcadeAttachment)
	var active *application.ArcadeAttachment
	for i := range attachments {
		entry := &attachments[i]
		if _, found := byMachine[entry.Machine]; !found {
			byMachine[entry.Machine] = entry
		}
		if active == nil && (entry.State == "QUEUED" || entry.State == "RUNNING") {
			active = entry
		}
	}
	return byMachine, active
}

func arcadeUnsupported(code string) bool {
	switch code {
	case "UNSUPPORTED_MERGED_ROMSET", "UNSUPPORTED_CHD", "ARCADE_DEPENDENCY_CYCLE", "ARCADE_DAT_UNAVAILABLE":
		return true
	default:
		return false
	}
}

package gamecontent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	model "retrom/internal/model/gamecontent"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/contentmanifest"
	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/engine/rpgmaker/detector"
	"retrom/internal/capability/engine/rpgmaker/fileset"
	"retrom/internal/capability/engine/rpgmaker/materializer"
)

type rpgReplacementIndex struct {
	sourceFiles []detector.File
	digests     map[string]string
	blobs       *blobstore.Store
}

func (index rpgReplacementIndex) Files() []detector.File {
	return append([]detector.File(nil), index.sourceFiles...)
}

func (index rpgReplacementIndex) Open(logicalPath string) (io.ReadCloser, error) {
	digest, exists := index.digests[logicalPath]
	if !exists {
		return nil, os.ErrNotExist
	}
	reader, err := index.blobs.OpenDigest(digest)
	if err != nil {
		return nil, fmt.Errorf("open RPG replacement file: %w", err)
	}
	return reader, nil
}

func (service *Service) prepareRPGMakerReplacement(
	ctx context.Context,
	snapshot model.JobSnapshot,
	files []model.UploadedFile,
) (model.PreparedReplacement, error) {
	if service.blobs == nil || snapshot.RPGGeneration == "" || len(files) == 0 {
		return model.PreparedReplacement{}, &replacementValidationError{code: "RPG_REPLACEMENT_INPUT_INVALID"}
	}
	project, profile, err := service.detectRPGMakerReplacement(files)
	if err != nil {
		return model.PreparedReplacement{}, err
	}
	if string(profile.ExpectedGeneration) != snapshot.RPGGeneration {
		return model.PreparedReplacement{}, &replacementValidationError{code: "RPG_REPLACEMENT_GENERATION_MISMATCH"}
	}
	projectFiles, sessionState := fileset.ExcludeSessionState(profile.ExpectedGeneration, project.Files)
	replacement, materializerSources, manifestFiles := buildRPGMakerReplacementFiles(
		files, projectFiles, service.blobs,
	)
	fingerprint, totalBytes, err := contentmanifest.FilesDigest(manifestFiles)
	if err != nil {
		return model.PreparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
	}
	manifest, manifestDigest, err := contentmanifest.Build(replacement.ContentKind, manifestFiles)
	if err != nil {
		return model.PreparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
	}
	requirementsJSON, requirementsSHA := rpgReplacementRequirements(profile)
	if requirementsSHA != snapshot.RPGRequirementsSHA256 {
		return model.PreparedReplacement{}, &replacementValidationError{code: "RPG_REPLACEMENT_DEPENDENCIES_CHANGED"}
	}
	excluded := append(append([]string(nil), project.RemovedNoise...), sessionState...)
	sort.Strings(excluded)
	analysis, err := rpgReplacementAnalysis(profile, project.Root, excluded, requirementsJSON)
	if err != nil {
		return model.PreparedReplacement{}, &replacementValidationError{code: "RPG_REPLACEMENT_INPUT_INVALID"}
	}
	variantFiles, err := service.materializeRPGMakerReplacement(ctx, profile.ExpectedGeneration, materializerSources)
	if err != nil {
		return model.PreparedReplacement{}, err
	}
	replacement.Manifest = manifest
	replacement.ManifestDigest = manifestDigest
	replacement.FirstContentLogicalName = replacement.Files[0].LogicalName
	replacement.RPGMaker = &model.PreparedRPGMakerReplacement{
		Profile: profile, ProjectRoot: project.Root, ExcludedFiles: excluded,
		FileCount: len(replacement.Files), TotalBytes: totalBytes, ProjectFingerprint: fingerprint,
		RequirementsSHA256: requirementsSHA, AnalysisJSON: analysis, VariantFiles: variantFiles,
	}
	return replacement, nil
}

func (service *Service) detectRPGMakerReplacement(
	files []model.UploadedFile,
) (fileset.Project, detector.Profile, error) {
	sources := make([]fileset.SourceFile, 0, len(files))
	for index, file := range files {
		sources = append(sources, fileset.SourceFile{
			Path: file.LogicalName, SizeBytes: file.SizeBytes, SourceIndex: index,
		})
	}
	project, err := fileset.NormalizeProject(sources)
	if err != nil {
		return fileset.Project{}, detector.Profile{}, rpgReplacementProjectError(err)
	}
	detectionIndex := rpgReplacementIndex{
		sourceFiles: make([]detector.File, 0, len(project.Files)),
		digests:     make(map[string]string, len(project.Files)), blobs: service.blobs,
	}
	for _, projectFile := range project.Files {
		source := files[projectFile.SourceIndex]
		detectionIndex.sourceFiles = append(detectionIndex.sourceFiles, detector.File{
			Path: projectFile.Path, Size: projectFile.SizeBytes,
		})
		detectionIndex.digests[projectFile.Path] = source.SHA256
	}
	profile, err := detector.Detect(detector.VirtualCoreID, detectionIndex)
	if err != nil {
		return fileset.Project{}, detector.Profile{}, rpgReplacementDetectionError(err)
	}
	return project, profile, nil
}

func buildRPGMakerReplacementFiles(
	files []model.UploadedFile,
	projectFiles []fileset.SourceFile,
	blobs *blobstore.Store,
) (model.PreparedReplacement, []materializer.SourceFile, []contentmanifest.File) {
	replacement := model.PreparedReplacement{ContentKind: string(contentprofile.ContentKindRPGMakerProject)}
	replacement.Files = make([]model.ReplacementFile, 0, len(projectFiles))
	materializerSources := make([]materializer.SourceFile, 0, len(projectFiles))
	manifestFiles := make([]contentmanifest.File, 0, len(projectFiles))
	for index, projectFile := range projectFiles {
		source := files[projectFile.SourceIndex]
		replacement.Files = append(replacement.Files, model.ReplacementFile{
			Role: "PROJECT_FILE", LogicalName: projectFile.Path, BlobID: source.BlobID,
			SHA256: source.SHA256, SizeBytes: source.SizeBytes, SortOrder: index,
		})
		digest := source.SHA256
		materializerSources = append(materializerSources, materializer.SourceFile{
			Path: projectFile.Path, Size: source.SizeBytes,
			Open: func() (io.ReadCloser, error) { return blobs.OpenDigest(digest) },
		})
		manifestFiles = append(manifestFiles, contentmanifest.File{
			Role: "PROJECT_FILE", LogicalName: projectFile.Path,
			BlobSHA256: source.SHA256, SizeBytes: source.SizeBytes,
		})
	}
	return replacement, materializerSources, manifestFiles
}

func (service *Service) materializeRPGMakerReplacement(
	ctx context.Context,
	generation detector.Generation,
	files []materializer.SourceFile,
) ([]model.PreparedRPGMakerVariantFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("materialize RPG replacement: %w", err)
	}
	switch generation {
	case detector.RPG2000, detector.RPG2003:
		index, err := materializer.BuildEasyRPGIndex(files)
		if err != nil {
			return nil, &replacementValidationError{code: "RPG_REPLACEMENT_MATERIALIZATION_FAILED"}
		}
		metadata, err := service.blobs.Put(bytes.NewReader(index.Contents))
		if err != nil {
			return nil, fmt.Errorf("materialize RPG replacement index: %w", err)
		}
		return []model.PreparedRPGMakerVariantFile{{
			Role: "RPG_EASYRPG_INDEX", LogicalName: "index.json", Metadata: metadata,
		}}, nil
	case detector.RPGXP, detector.RPGVX, detector.RPGVXAce:
		metadata, err := service.writeRPGMakerReplacementArchive(files)
		if err != nil {
			return nil, err
		}
		return []model.PreparedRPGMakerVariantFile{{
			Role: "RPG_MAKER_LAUNCH_BUNDLE", LogicalName: "game.mkxpz", Metadata: metadata,
		}}, nil
	case detector.RPGMV, detector.RPGMZ:
		return nil, nil
	default:
		return nil, &replacementValidationError{code: "RPG_REPLACEMENT_GENERATION_MISMATCH"}
	}
}

func (service *Service) writeRPGMakerReplacementArchive(
	files []materializer.SourceFile,
) (blobstore.Metadata, error) {
	reader, writer := io.Pipe()
	type buildResult struct {
		result materializer.Result
		err    error
	}
	finished := make(chan buildResult, 1)
	go func() {
		result, err := materializer.WriteMKXPZ(writer, files)
		if err != nil {
			_ = writer.CloseWithError(err)
		} else {
			err = writer.Close()
		}
		finished <- buildResult{result: result, err: err}
	}()
	metadata, putErr := service.blobs.Put(reader)
	built := <-finished
	if putErr != nil || built.err != nil || metadata.SHA256 != built.result.SHA256 ||
		metadata.Size != built.result.SizeBytes {
		return blobstore.Metadata{}, fmt.Errorf(
			"materialize RPG replacement archive: %w",
			errors.Join(putErr, built.err, materializer.ErrInvalid),
		)
	}
	return metadata, nil
}

func rpgReplacementRequirements(profile detector.Profile) ([]byte, string) {
	type rtpRequirement struct {
		Slot           int    `json:"slot"`
		DeclaredName   string `json:"declaredName"`
		NormalizedName string `json:"normalizedName"`
	}
	requirements := append([]detector.Requirement(nil), profile.Requirements...)
	if requirements == nil {
		requirements = []detector.Requirement{}
	}
	rtp := make([]rtpRequirement, 0, len(profile.RTPDependencies))
	for _, dependency := range profile.RTPDependencies {
		rtp = append(rtp, rtpRequirement{
			Slot: dependency.Slot, DeclaredName: dependency.DeclaredName,
			NormalizedName: dependency.NormalizedName,
		})
	}
	contents, _ := json.Marshal(map[string]any{"requirements": requirements, "rtpDependencies": rtp})
	digest := sha256.Sum256(contents)
	return contents, hex.EncodeToString(digest[:])
}

func rpgReplacementAnalysis(
	profile detector.Profile,
	root string,
	excluded []string,
	requirementsJSON []byte,
) ([]byte, error) {
	evidenceGeneration := any(nil)
	if profile.EvidenceGeneration != nil {
		evidenceGeneration = *profile.EvidenceGeneration
	}
	var requirements any
	if json.Unmarshal(requirementsJSON, &requirements) != nil {
		return nil, fmt.Errorf("%w: decode RPG replacement requirements", model.ErrInvalid)
	}
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "selectedCoreId": profile.SelectedCoreID,
		"expectedGeneration": profile.ExpectedGeneration, "evidenceGeneration": evidenceGeneration,
		"evidenceFamily": profile.EvidenceFamily, "evidenceConfidence": profile.EvidenceConfidence,
		"engineVersion": nullableRPGReplacementString(profile.EngineVersion),
		"markerPaths":   profile.MarkerPaths, "selfContained": profile.SelfContained,
		"requirements": requirements,
		"projectRoot":  root, "excludedFiles": excluded,
	})
	if err != nil {
		return nil, fmt.Errorf("encode RPG replacement analysis: %w", err)
	}
	return contents, nil
}

func nullableRPGReplacementString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func rpgReplacementProjectError(err error) error {
	var projectErr *fileset.ProjectError
	if errors.As(err, &projectErr) {
		return &replacementValidationError{code: string(projectErr.Code)}
	}
	return &replacementValidationError{code: "RPG_REPLACEMENT_INPUT_INVALID"}
}

func rpgReplacementDetectionError(err error) error {
	var detectionErr *detector.Error
	if errors.As(err, &detectionErr) {
		return &replacementValidationError{code: string(detectionErr.Code)}
	}
	return &replacementValidationError{code: "RPG_REPLACEMENT_INPUT_INVALID"}
}

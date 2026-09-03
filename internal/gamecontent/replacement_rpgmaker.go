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

	"retrom/internal/blobstore"
	"retrom/internal/contentmanifest"
	"retrom/internal/contentprofile"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/fileset"
	"retrom/internal/rpgmaker/materializer"
)

type preparedRPGMakerReplacement struct {
	profile            detector.Profile
	projectRoot        string
	excludedFiles      []string
	fileCount          int
	totalBytes         int64
	projectFingerprint string
	requirementsSHA256 string
	analysisJSON       []byte
	variantFiles       []preparedRPGMakerVariantFile
}

type preparedRPGMakerVariantFile struct {
	role, logicalName string
	metadata          blobstore.Metadata
}

type rpgReplacementIndex struct {
	files   []detector.File
	digests map[string]string
	blobs   *blobstore.Store
}

func (index rpgReplacementIndex) Files() []detector.File {
	return append([]detector.File(nil), index.files...)
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
	snapshot jobSnapshot,
	files []uploadedFile,
) (preparedReplacement, error) {
	if service.blobs == nil || snapshot.RPGGeneration == "" || len(files) == 0 {
		return preparedReplacement{}, &replacementValidationError{code: "RPG_REPLACEMENT_INPUT_INVALID"}
	}
	project, profile, err := service.detectRPGMakerReplacement(files)
	if err != nil {
		return preparedReplacement{}, err
	}
	if string(profile.ExpectedGeneration) != snapshot.RPGGeneration {
		return preparedReplacement{}, &replacementValidationError{code: "RPG_REPLACEMENT_GENERATION_MISMATCH"}
	}
	projectFiles, sessionState := fileset.ExcludeSessionState(profile.ExpectedGeneration, project.Files)
	replacement, materializerSources, manifestFiles := buildRPGMakerReplacementFiles(
		files, projectFiles, service.blobs,
	)
	fingerprint, totalBytes, err := contentmanifest.FilesDigest(manifestFiles)
	if err != nil {
		return preparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
	}
	manifest, manifestDigest, err := contentmanifest.Build(replacement.contentKind, manifestFiles)
	if err != nil {
		return preparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
	}
	requirementsJSON, requirementsSHA := rpgReplacementRequirements(profile)
	if requirementsSHA != snapshot.RPGRequirementsSHA256 {
		return preparedReplacement{}, &replacementValidationError{code: "RPG_REPLACEMENT_DEPENDENCIES_CHANGED"}
	}
	excluded := append(append([]string(nil), project.RemovedNoise...), sessionState...)
	sort.Strings(excluded)
	analysis, err := rpgReplacementAnalysis(profile, project.Root, excluded, requirementsJSON)
	if err != nil {
		return preparedReplacement{}, &replacementValidationError{code: "RPG_REPLACEMENT_INPUT_INVALID"}
	}
	variantFiles, err := service.materializeRPGMakerReplacement(ctx, profile.ExpectedGeneration, materializerSources)
	if err != nil {
		return preparedReplacement{}, err
	}
	replacement.manifest = manifest
	replacement.manifestDigest = manifestDigest
	replacement.firstContentLogicalName = replacement.files[0].logicalName
	replacement.rpgMaker = &preparedRPGMakerReplacement{
		profile: profile, projectRoot: project.Root, excludedFiles: excluded,
		fileCount: len(replacement.files), totalBytes: totalBytes, projectFingerprint: fingerprint,
		requirementsSHA256: requirementsSHA, analysisJSON: analysis, variantFiles: variantFiles,
	}
	return replacement, nil
}

func (service *Service) detectRPGMakerReplacement(
	files []uploadedFile,
) (fileset.Project, detector.Profile, error) {
	sources := make([]fileset.SourceFile, 0, len(files))
	for index, file := range files {
		sources = append(sources, fileset.SourceFile{
			Path: file.logicalName, SizeBytes: file.sizeBytes, SourceIndex: index,
		})
	}
	project, err := fileset.NormalizeProject(sources)
	if err != nil {
		return fileset.Project{}, detector.Profile{}, rpgReplacementProjectError(err)
	}
	detectionIndex := rpgReplacementIndex{
		files:   make([]detector.File, 0, len(project.Files)),
		digests: make(map[string]string, len(project.Files)), blobs: service.blobs,
	}
	for _, projectFile := range project.Files {
		source := files[projectFile.SourceIndex]
		detectionIndex.files = append(detectionIndex.files, detector.File{
			Path: projectFile.Path, Size: projectFile.SizeBytes,
		})
		detectionIndex.digests[projectFile.Path] = source.sha256
	}
	profile, err := detector.Detect(detector.VirtualCoreID, detectionIndex)
	if err != nil {
		return fileset.Project{}, detector.Profile{}, rpgReplacementDetectionError(err)
	}
	return project, profile, nil
}

func buildRPGMakerReplacementFiles(
	files []uploadedFile,
	projectFiles []fileset.SourceFile,
	blobs *blobstore.Store,
) (preparedReplacement, []materializer.SourceFile, []contentmanifest.File) {
	replacement := preparedReplacement{contentKind: string(contentprofile.ContentKindRPGMakerProject)}
	replacement.files = make([]replacementFile, 0, len(projectFiles))
	materializerSources := make([]materializer.SourceFile, 0, len(projectFiles))
	manifestFiles := make([]contentmanifest.File, 0, len(projectFiles))
	for index, projectFile := range projectFiles {
		source := files[projectFile.SourceIndex]
		replacement.files = append(replacement.files, replacementFile{
			role: "PROJECT_FILE", logicalName: projectFile.Path, blobID: source.blobID,
			sha256: source.sha256, sizeBytes: source.sizeBytes, sortOrder: index,
		})
		digest := source.sha256
		materializerSources = append(materializerSources, materializer.SourceFile{
			Path: projectFile.Path, Size: source.sizeBytes,
			Open: func() (io.ReadCloser, error) { return blobs.OpenDigest(digest) },
		})
		manifestFiles = append(manifestFiles, contentmanifest.File{
			Role: "PROJECT_FILE", LogicalName: projectFile.Path,
			BlobSHA256: source.sha256, SizeBytes: source.sizeBytes,
		})
	}
	return replacement, materializerSources, manifestFiles
}

func (service *Service) materializeRPGMakerReplacement(
	ctx context.Context,
	generation detector.Generation,
	files []materializer.SourceFile,
) ([]preparedRPGMakerVariantFile, error) {
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
		return []preparedRPGMakerVariantFile{{
			role: "RPG_EASYRPG_INDEX", logicalName: "index.json", metadata: metadata,
		}}, nil
	case detector.RPGXP, detector.RPGVX, detector.RPGVXAce:
		metadata, err := service.writeRPGMakerReplacementArchive(files)
		if err != nil {
			return nil, err
		}
		return []preparedRPGMakerVariantFile{{
			role: "RPG_MAKER_LAUNCH_BUNDLE", logicalName: "game.mkxpz", metadata: metadata,
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
		return nil, fmt.Errorf("%w: decode RPG replacement requirements", ErrInvalid)
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

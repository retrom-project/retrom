package serverimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/firmware"
	"retrom/internal/importing"
	"retrom/internal/serversource"
)

type association struct {
	item catalogItem
	kind string
}

type candidateHashTask struct {
	file         discoveredFile
	associations []association
}

type candidateHashResult struct {
	candidates  []*evaluatedCandidate
	hashedBytes int64
	err         error
}

type candidateWalkResult struct {
	counts walkCounts
	err    error
}

// Bounded walker, hash pool and archive evaluation coordinate one discovery barrier.
func (service *Service) discoverCandidates(
	ctx context.Context,
	unit work,
	directory *os.File,
	items []catalogItem,
) (map[string][]*evaluatedCandidate, walkCounts, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	index := newCandidateIndex(items)
	tasks, results := service.startCandidateHashWorkers(ctx, unit)
	walkDone := service.startCandidateWalk(ctx, unit, directory, index, tasks)
	grouped, firstErr := service.collectCandidateResults(cancel, results)
	walked := <-walkDone
	if walked.err != nil && firstErr == nil {
		firstErr = walked.err
	}
	return grouped, walked.counts, firstErr
}

func (service *Service) startCandidateHashWorkers(
	ctx context.Context,
	unit work,
) (chan<- candidateHashTask, <-chan candidateHashResult) {
	tasks := make(chan candidateHashTask, 4)
	results := make(chan candidateHashResult, 4)
	var workers sync.WaitGroup
	for range service.scanLimits.hashWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range tasks {
				result := service.hashDiscoveredCandidate(ctx, unit, task)
				cleanup.Error("close", task.file.Parent.Close())
				results <- result
			}
		}()
	}
	go func() {
		workers.Wait()
		close(results)
	}()
	return tasks, results
}

func (service *Service) startCandidateWalk(
	ctx context.Context,
	unit work,
	directory *os.File,
	index candidateIndex,
	tasks chan<- candidateHashTask,
) <-chan candidateWalkResult {
	walkDone := make(chan candidateWalkResult, 1)
	go func() {
		var physicalCandidates int64
		var hashPhase sync.Once
		counts, err := walkFiles(directory, service.scanLimits, func(file discoveredFile) error {
			if service.cancelRequested(ctx, unit.JobID) {
				return errCancelled
			}
			matched := index.associations(file)
			if len(matched) == 0 {
				return nil
			}
			physicalCandidates++
			if physicalCandidates > service.scanLimits.maxPhysicalCandidates {
				return ErrScanLimit
			}
			parent, duplicateErr := duplicateDirectory(file.Parent)
			if duplicateErr != nil {
				return ErrRootUnavailable
			}
			file.Parent = parent
			hashPhase.Do(func() { service.progress(ctx, unit, "HASHING", 0, physicalCandidates) })
			select {
			case tasks <- candidateHashTask{file: file, associations: matched}:
				return nil
			case <-ctx.Done():
				cleanup.Error("close", parent.Close())
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return errExecutionDeadline
				}
				return ctx.Err()
			}
		})
		close(tasks)
		walkDone <- candidateWalkResult{counts: counts, err: err}
	}()
	return walkDone
}

func (service *Service) collectCandidateResults(
	cancel context.CancelFunc,
	results <-chan candidateHashResult,
) (map[string][]*evaluatedCandidate, error) {
	grouped := make(map[string][]*evaluatedCandidate)
	var firstErr error
	var hashedBytes int64
	for result := range results {
		if result.err != nil && firstErr == nil {
			firstErr = result.err
			cancel()
		}
		hashedBytes += result.hashedBytes
		if hashedBytes > service.scanLimits.maxHashedBytes && firstErr == nil {
			firstErr = ErrScanLimit
			cancel()
		}
		for _, candidate := range result.candidates {
			values := grouped[candidate.Item.RequirementID]
			values = append(values, candidate)
			grouped[candidate.Item.RequirementID] = values
			if len(values) > service.scanLimits.maxCandidatesPerRequirement && firstErr == nil {
				firstErr = ErrScanLimit
				cancel()
			}
		}
	}
	return grouped, firstErr
}

// Read, re-stat and per-association evaluation branches are independent source-safety checks.
func (service *Service) hashDiscoveredCandidate(
	ctx context.Context,
	unit work,
	task candidateHashTask,
) candidateHashResult {
	result := candidateHashResult{}
	release, acquireErr := serversource.AcquireReader(ctx)
	if acquireErr != nil {
		result.err = acquireErr
		return result
	}
	defer release()
	handle, before, openErr := openCandidate(task.file)
	if openErr != nil {
		return failedCandidateResult(task, "READ_FAILED")
	}
	metadata, putErr := service.blobs.Put(&cancelReader{
		ctx: ctx, reader: handle, check: func() bool { return service.pollCancellation(ctx, unit) },
	})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if errors.Is(putErr, errCancelled) || errors.Is(putErr, errExecutionDeadline) ||
		errors.Is(putErr, context.Canceled) || errors.Is(putErr, context.DeadlineExceeded) {
		result.err = putErr
		return result
	}
	if putErr != nil || statErr != nil || !sameFileFacts(before, after) {
		return failedCandidateResult(task, "SOURCE_CHANGED")
	}
	result.hashedBytes = metadata.Size
	facts := firmware.FileFacts{
		RelativePath: task.file.RelativePath, Basename: task.file.Basename, SizeBytes: metadata.Size,
		MD5: metadata.MD5, SHA1: metadata.SHA1, SHA256: metadata.SHA256, CRC32: metadata.CRC32,
	}
	result.candidates, result.err = service.evaluateCandidateAssociations(ctx, task, metadata, facts)
	return result
}

func failedCandidateResult(task candidateHashTask, code string) candidateHashResult {
	result := candidateHashResult{}
	for _, association := range task.associations {
		result.candidates = append(
			result.candidates,
			failedCandidate(association.item, task.file, association.kind, code),
		)
	}
	return result
}

func (service *Service) evaluateCandidateAssociations(
	ctx context.Context,
	task candidateHashTask,
	metadata blobstore.Metadata,
	facts firmware.FileFacts,
) ([]*evaluatedCandidate, error) {
	result := make([]*evaluatedCandidate, 0, len(task.associations))
	for _, association := range task.associations {
		candidate, err := service.evaluate(ctx, association.item, association.kind, task.file, metadata, facts)
		if err != nil {
			if errors.Is(err, errExecutionDeadline) || errors.Is(err, context.Canceled) ||
				errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			candidate = failedCandidate(association.item, task.file, association.kind, "ARCHIVE_UNSAFE")
			candidate.Metadata = metadata
		}
		if association.kind != "RENAMED_HASH_MATCH" || candidate.Static != nil && candidate.Static.ExactHash {
			result = append(result, candidate)
		}
	}
	return result, nil
}

type candidateIndex struct {
	byName map[string][]catalogItem
	bySize map[int64][]catalogItem
}

func newCandidateIndex(items []catalogItem) candidateIndex {
	result := candidateIndex{byName: make(map[string][]catalogItem), bySize: make(map[int64][]catalogItem)}
	for _, item := range items {
		if item.SourceKind != "DAT_MACHINE" || strings.HasSuffix(importing.ASCIICaseFold(item.LogicalName), ".zip") {
			key := importing.ASCIICaseFold(item.LogicalName)
			result.byName[key] = append(result.byName[key], item)
		}
		if item.SourceKind == "STATIC" && item.ExpectedSize != nil &&
			(item.ExpectedMD5 != nil || item.ExpectedSHA1 != nil || item.ExpectedSHA256 != nil) {
			result.bySize[*item.ExpectedSize] = append(result.bySize[*item.ExpectedSize], item)
		}
	}
	return result
}

func (index candidateIndex) associations(file discoveredFile) []association {
	result := make([]association, 0)
	folded := importing.ASCIICaseFold(file.Basename)
	for _, item := range index.byName[folded] {
		if item.SourceKind != "DAT_MACHINE" || strings.HasSuffix(folded, ".zip") {
			kind := "CASEFOLD_NAME"
			if file.Basename == item.LogicalName {
				kind = "EXACT_NAME"
			}
			result = append(result, association{item: item, kind: kind})
		}
	}
	for _, item := range index.bySize[file.SizeBytes] {
		if folded != importing.ASCIICaseFold(item.LogicalName) {
			result = append(result, association{item: item, kind: "RENAMED_HASH_MATCH"})
		}
	}
	return result
}

func (service *Service) evaluate(ctx context.Context, item catalogItem, association string, file discoveredFile,
	metadata blobstore.Metadata, facts firmware.FileFacts,
) (*evaluatedCandidate, error) {
	id, _ := uuid.NewV7()
	candidate := &evaluatedCandidate{
		ID:          id.String(),
		Item:        item,
		File:        file,
		Association: association,
		Metadata:    metadata,
		State:       "ELIGIBLE",
	}
	if item.SourceKind == "STATIC" {
		expectation := firmware.StaticExpectation{LogicalName: item.LogicalName, SizeBytes: item.ExpectedSize}
		if item.ExpectedMD5 != nil {
			expectation.MD5 = *item.ExpectedMD5
		}
		if item.ExpectedSHA1 != nil {
			expectation.SHA1 = *item.ExpectedSHA1
		}
		if item.ExpectedSHA256 != nil {
			expectation.SHA256 = *item.ExpectedSHA256
		}
		evaluation := firmware.EvaluateStatic(expectation, facts)
		candidate.Static = &evaluation
		candidate.Details = map[string]any{
			"schemaVersion":       1,
			"exactHash":           evaluation.ExactHash,
			"expectedSizeMatched": evaluation.ExpectedSizeMatched,
			"exactBasename":       evaluation.ExactBasename,
		}
		return candidate, nil
	}
	select {
	case service.archiveScan <- struct{}{}:
		defer func() { <-service.archiveScan }()
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return candidate, errExecutionDeadline
		}
		return candidate, fmt.Errorf("wait for archive scan slot: %w", ctx.Err())
	}
	entries, err := importing.ScanZIP(ctx, metadata.Path, importing.DefaultArchiveLimits())
	if err != nil {
		return candidate, fmt.Errorf("scan server import ZIP candidate: %w", err)
	}
	expected, err := service.expectedDATEntries(ctx, item)
	if err != nil {
		return candidate, err
	}
	evaluation := firmware.EvaluateDAT(item.LogicalName, expected, facts, entries)
	candidate.DAT = &evaluation
	candidate.ExpectedDATEntries = expected
	candidate.ArchiveEntries = entries
	candidate.Details = map[string]any{
		"schemaVersion":   1,
		"launchable":      evaluation.Launchable,
		"matchedCount":    evaluation.MatchedCount,
		"aliasedCount":    evaluation.AliasedCount,
		"mismatchedCount": evaluation.MismatchedCount,
		"missingCount":    evaluation.MissingCount,
		"extraCount":      evaluation.ExtraCount,
		"exactBasename":   evaluation.ExactBasename,
	}
	return candidate, nil
}

func staticExpectation(item catalogItem) *firmware.StaticExpectation {
	if item.SourceKind != "STATIC" {
		return nil
	}
	result := &firmware.StaticExpectation{LogicalName: item.LogicalName, SizeBytes: item.ExpectedSize}
	if item.ExpectedMD5 != nil {
		result.MD5 = *item.ExpectedMD5
	}
	if item.ExpectedSHA1 != nil {
		result.SHA1 = *item.ExpectedSHA1
	}
	if item.ExpectedSHA256 != nil {
		result.SHA256 = *item.ExpectedSHA256
	}
	return result
}

func markDuplicateBytes(candidates []*evaluatedCandidate) {
	ranked := rankCandidates(candidates)
	seen := make(map[string]struct{}, len(ranked))
	for _, candidate := range ranked {
		if candidate.Metadata.SHA256 == "" {
			continue
		}
		if _, exists := seen[candidate.Metadata.SHA256]; exists {
			candidate.State = "DUPLICATE_BYTES"
			candidate.Details["code"] = "BIOS_CANDIDATE_DUPLICATE_BYTES"
			continue
		}
		seen[candidate.Metadata.SHA256] = struct{}{}
	}
}

func sameMetadata(left, right blobstore.Metadata) bool {
	return left.Size == right.Size && left.MD5 == right.MD5 && left.SHA1 == right.SHA1 &&
		left.SHA256 == right.SHA256 && left.CRC32 == right.CRC32
}

func (service *Service) verifySelected(
	ctx context.Context,
	unit work,
	root Root,
	selected *evaluatedCandidate,
) (*evaluatedCandidate, error) {
	release, acquireErr := serversource.AcquireReader(ctx)
	if acquireErr != nil {
		return nil, fmt.Errorf("serverimport/acquire reader: %w", acquireErr)
	}
	defer release()
	handle, before, err := openRelativeCandidate(root.path, unit.RelativePath, selected.File.RelativePath)
	if err != nil {
		return nil, errSourceChanged
	}
	metadata, putErr := service.blobs.Put(&cancelReader{
		ctx: ctx, reader: handle, check: func() bool { return service.pollCancellation(ctx, unit) },
	})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if errors.Is(putErr, errCancelled) {
		return nil, errCancelled
	}
	if putErr != nil || statErr != nil || !sameFileFacts(before, after) || !sameMetadata(metadata, selected.Metadata) {
		return nil, errSourceChanged
	}
	facts := firmware.FileFacts{
		RelativePath: selected.File.RelativePath, Basename: selected.File.Basename, SizeBytes: metadata.Size,
		MD5: metadata.MD5, SHA1: metadata.SHA1, SHA256: metadata.SHA256, CRC32: metadata.CRC32,
	}
	verified, err := service.evaluate(ctx, selected.Item, selected.Association, selected.File, metadata, facts)
	if err != nil || verified.State != "ELIGIBLE" {
		return nil, errSourceChanged
	}
	verified.ID = selected.ID
	if selected.Static != nil {
		if verified.Static == nil || firmware.CompareStatic(*verified.Static, *selected.Static) != 0 {
			return nil, errSourceChanged
		}
	} else if verified.DAT == nil || firmware.CompareDAT(*verified.DAT, *selected.DAT) != 0 {
		return nil, errSourceChanged
	}
	return verified, nil
}

func failedCandidate(item catalogItem, file discoveredFile, association, state string) *evaluatedCandidate {
	id, _ := uuid.NewV7()
	code := "SERVER_IMPORT_SOURCE_UNREADABLE"
	switch state {
	case "SOURCE_CHANGED":
		code = "SERVER_IMPORT_SOURCE_CHANGED"
	case "ARCHIVE_UNSAFE":
		code = "ARCHIVE_UNSAFE"
	}
	return &evaluatedCandidate{
		ID: id.String(), Item: item, File: file, Association: association, State: state,
		Details: map[string]any{"schemaVersion": 1, "code": code},
	}
}

type cancelReader struct {
	ctx    context.Context
	reader io.Reader
	check  func() bool
	since  int64
}

//nolint:wrapcheck // io.Reader must return the underlying io.EOF sentinel unchanged to satisfy io.Copy.
func (reader *cancelReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, errExecutionDeadline
		}
		return 0, fmt.Errorf("candidate read context: %w", err)
	}
	if reader.since >= 8<<20 {
		reader.since = 0
		if reader.check != nil && reader.check() {
			return 0, errCancelled
		}
	}
	remaining := int64(8<<20) - reader.since
	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	count, err := reader.reader.Read(buffer)
	reader.since += int64(count)
	return count, err
}

func (service *Service) expectedDATEntries(ctx context.Context, item catalogItem) ([]firmware.ExpectedDATEntry, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT entry.name,entry.size_bytes,entry.crc32,entry.sha1
FROM dat_rom_entries entry
WHERE entry.dat_version_id=? AND entry.machine_name=? AND COALESCE(entry.status,'GOOD')<>'NODUMP'
AND (entry.bios_name IS NULL OR EXISTS(
 SELECT 1 FROM dat_bios_sets bios WHERE bios.dat_version_id=entry.dat_version_id
 AND bios.machine_name=entry.machine_name AND bios.bios_name=entry.bios_name AND bios.is_default=1
)) ORDER BY entry.name COLLATE BINARY,entry.ordinal
`, item.DATVersionID, item.DATMachineName)
	if err != nil {
		return nil, fmt.Errorf("query expected DAT entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]firmware.ExpectedDATEntry, 0)
	for rows.Next() {
		var entry firmware.ExpectedDATEntry
		var crc, sha sql.NullString
		if err := rows.Scan(&entry.Name, &entry.SizeBytes, &crc, &sha); err != nil {
			return nil, fmt.Errorf("scan expected DAT entry: %w", err)
		}
		if crc.Valid {
			entry.CRC32 = crc.String
		}
		if sha.Valid {
			entry.SHA1 = sha.String
		}
		result = append(result, entry)
	}
	if len(result) == 0 {
		return nil, ErrCatalogInvalid
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expected DAT entries: %w", err)
	}
	return result, nil
}

package importing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/bodgit/sevenzip"

	"retrom/internal/cleanup"
)

type sevenZipBatchProcess struct {
	ctx     context.Context
	cancel  context.CancelFunc
	archive *os.File
	command *exec.Cmd
	stdout  io.ReadCloser
}

// ExtractSevenZipEntries extracts a set of already-scanned entries through one
// sandboxed worker. Keeping one sevenzip.Reader alive lets solid archives reuse
// their decoder instead of decompressing the same folder once per project file.
func ExtractSevenZipEntries(
	ctx context.Context,
	path string,
	expected []ArchiveEntry,
	consume func(ArchiveEntry, io.Reader) error,
) error {
	ordered, ordinals, err := orderedSevenZipEntries(expected)
	if err != nil {
		return err
	}
	if len(ordered) == 0 {
		return nil
	}
	process, err := startSevenZipBatchProcess(ctx, path, ordinals)
	if err != nil {
		return err
	}
	defer process.close()
	for _, entry := range ordered {
		if err := process.consume(entry, consume); err != nil {
			return err
		}
	}
	return process.finish()
}

func startSevenZipBatchProcess(ctx context.Context, path, ordinals string) (*sevenZipBatchProcess, error) {
	workerContext, cancel := context.WithTimeout(ctx, archiveWorkerTimeout)
	archive, err := openSevenZip(path)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open 7z: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		cancel()
		cleanup.Error("close", archive.Close())
		return nil, fmt.Errorf("archive worker executable: %w", err)
	}
	command := exec.CommandContext(workerContext, executable, archiveWorkerCommand, "extract-batch", ordinals)
	command.ExtraFiles = []*os.File{archive}
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		cleanup.Error("close", archive.Close())
		return nil, fmt.Errorf("archive worker stdout: %w", err)
	}
	if err := command.Start(); err != nil {
		workerErr := workerExecutionError(workerContext, err)
		cancel()
		cleanup.Error("close", archive.Close())
		return nil, workerErr
	}
	return &sevenZipBatchProcess{
		ctx: workerContext, cancel: cancel, archive: archive, command: command, stdout: stdout,
	}, nil
}

func (process *sevenZipBatchProcess) consume(
	entry ArchiveEntry,
	consume func(ArchiveEntry, io.Reader) error,
) error {
	response, readErr := readWorkerResponse(process.stdout)
	if readErr != nil {
		return stopBatchWorker(process.ctx, process.command, readErr)
	}
	if response.ErrorCode != "" {
		_ = process.command.Process.Kill()
		_ = process.command.Wait()
		return archiveError(response.ErrorCode)
	}
	if response.Ordinal == nil || *response.Ordinal != entry.Ordinal || response.Size != entry.Size {
		return stopBatchWorker(process.ctx, process.command, ErrArchiveUnsafe)
	}
	limited := &io.LimitedReader{R: process.stdout, N: entry.Size}
	if err := consume(entry, limited); err != nil {
		_ = process.command.Process.Kill()
		_ = process.command.Wait()
		return fmt.Errorf("consume 7z entry %d: %w", entry.Ordinal, err)
	}
	if limited.N != 0 {
		return stopBatchWorker(process.ctx, process.command, ErrArchiveUnsafe)
	}
	return nil
}

func (process *sevenZipBatchProcess) finish() error {
	var trailing [1]byte
	trailingCount, trailingErr := process.stdout.Read(trailing[:])
	waitErr := process.command.Wait()
	if trailingCount != 0 || !errors.Is(trailingErr, io.EOF) || waitErr != nil {
		return workerExecutionError(process.ctx, errors.Join(trailingErr, waitErr))
	}
	return nil
}

func (process *sevenZipBatchProcess) close() {
	process.cancel()
	cleanup.Error("close", process.archive.Close())
}

func orderedSevenZipEntries(expected []ArchiveEntry) ([]ArchiveEntry, string, error) {
	if len(expected) > DefaultArchiveLimits().MaxEntries {
		return nil, "", ErrArchiveLimitExceeded
	}
	ordered := append([]ArchiveEntry(nil), expected...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].Ordinal < ordered[right].Ordinal })
	parts := make([]string, len(ordered))
	for index, entry := range ordered {
		if entry.ArchiveFormat != "SEVEN_Z" || entry.Ordinal < 0 || entry.Size < 0 ||
			entry.Size > DefaultArchiveLimits().MaxEntryBytes || index > 0 && ordered[index-1].Ordinal == entry.Ordinal {
			return nil, "", ErrArchiveUnsafe
		}
		parts[index] = strconv.Itoa(entry.Ordinal)
	}
	return ordered, strings.Join(parts, ","), nil
}

func stopBatchWorker(ctx context.Context, command *exec.Cmd, cause error) error {
	_ = command.Process.Kill()
	waitErr := command.Wait()
	if waitErr != nil || ctx.Err() != nil {
		return workerExecutionError(ctx, errors.Join(cause, waitErr))
	}
	return ErrArchiveUnsafe
}

func runArchiveBatchExtractWorker(archive *os.File, size int64, arguments []string) error {
	ordinals, err := parseBatchOrdinals(arguments)
	if err != nil {
		return writeWorkerResponse(os.Stdout, archiveWorkerResponse{ErrorCode: errorCode(err)})
	}
	reader, err := sevenzip.NewReader(archive, size)
	if err != nil {
		return writeWorkerResponse(os.Stdout, archiveWorkerResponse{
			ErrorCode: errorCode(classifySevenZipReadError(err, false)),
		})
	}
	for _, ordinal := range ordinals {
		if ordinal >= len(reader.File) {
			return writeWorkerResponse(os.Stdout, archiveWorkerResponse{ErrorCode: errorCode(ErrArchiveUnsafe)})
		}
		item := reader.File[ordinal]
		if item.UncompressedSize > uint64(DefaultArchiveLimits().MaxEntryBytes) {
			return writeWorkerResponse(os.Stdout, archiveWorkerResponse{
				ErrorCode: errorCode(ErrArchiveLimitExceeded),
			})
		}
		entryReader, openErr := item.Open()
		if openErr != nil {
			return writeWorkerResponse(os.Stdout, archiveWorkerResponse{
				ErrorCode: errorCode(classifySevenZipReadError(openErr, true)),
			})
		}
		entrySize := int64(item.UncompressedSize)
		ordinalCopy := ordinal
		if err := writeWorkerResponse(os.Stdout, archiveWorkerResponse{
			Ordinal: &ordinalCopy, Size: entrySize,
		}); err != nil {
			cleanup.Error("close", entryReader.Close())
			return err
		}
		written, copyErr := io.CopyN(os.Stdout, entryReader, entrySize)
		closeErr := entryReader.Close()
		if copyErr != nil || closeErr != nil || written != entrySize {
			return errors.Join(copyErr, closeErr, ErrArchiveUnsafe)
		}
	}
	return nil
}

func parseBatchOrdinals(arguments []string) ([]int, error) {
	if len(arguments) != 1 || arguments[0] == "" {
		return nil, ErrArchiveUnsafe
	}
	parts := strings.Split(arguments[0], ",")
	if len(parts) > DefaultArchiveLimits().MaxEntries {
		return nil, ErrArchiveLimitExceeded
	}
	ordinals := make([]int, len(parts))
	for index, part := range parts {
		ordinal, err := strconv.Atoi(part)
		if err != nil || ordinal < 0 || strconv.Itoa(ordinal) != part || index > 0 && ordinal <= ordinals[index-1] {
			return nil, ErrArchiveUnsafe
		}
		ordinals[index] = ordinal
	}
	return ordinals, nil
}

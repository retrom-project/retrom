package importing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/bodgit/sevenzip"

	"retrom/internal/cleanup"
	"retrom/internal/legacychecksum"
)

const (
	archiveWorkerCommand = "__archive-worker"
	archiveWorkerFD      = 3
	maxWorkerHeaderBytes = 64 << 20
	archiveWorkerTimeout = 125 * time.Second
)

var sevenZipMagic = []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}

type archiveWorkerResponse struct {
	ErrorCode string         `json:"errorCode,omitempty"`
	Entries   []ArchiveEntry `json:"entries,omitempty"`
	Size      int64          `json:"size,omitempty"`
}

type archiveWorkerCommandFactory func(context.Context, string, ...string) *exec.Cmd

// RunArchiveWorker executes the private archive worker command. The caller must
// pass the archive as inherited descriptor 3; filesystem paths are never
// accepted by the worker protocol.
//
// The two private worker protocols independently validate every argument and failure response.
func RunArchiveWorker(arguments []string) (bool, error) {
	if len(arguments) == 0 || arguments[0] != archiveWorkerCommand {
		return false, nil
	}
	if len(arguments) < 2 || len(arguments) > 7 {
		return true, ErrArchiveUnsafe
	}
	if err := applyArchiveWorkerLimits(); err != nil {
		return true, writeWorkerResponse(os.Stdout, archiveWorkerResponse{ErrorCode: errorCode(ErrArchiveSandboxUnavailable)})
	}
	archive := os.NewFile(archiveWorkerFD, "archive")
	if archive == nil {
		return true, writeWorkerResponse(os.Stdout, archiveWorkerResponse{ErrorCode: errorCode(ErrArchiveUnsafe)})
	}
	defer func() { cleanup.Error("close", archive.Close()) }()
	info, err := archive.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return true, writeWorkerResponse(os.Stdout, archiveWorkerResponse{ErrorCode: errorCode(ErrArchiveUnsafe)})
	}
	switch arguments[1] {
	case "scan":
		return true, runArchiveScanWorker(archive, info.Size(), arguments[2:])
	case "extract":
		return true, runArchiveExtractWorker(archive, info.Size(), arguments)
	default:
		return true, writeWorkerResponse(os.Stdout, archiveWorkerResponse{ErrorCode: errorCode(ErrArchiveUnsafe)})
	}
}

func runArchiveScanWorker(archive *os.File, size int64, arguments []string) error {
	limits, err := archiveLimitsFromArguments(arguments)
	if err != nil {
		return writeWorkerResponse(os.Stdout, archiveWorkerResponse{ErrorCode: errorCode(ErrArchiveUnsafe)})
	}
	entries, err := scanSevenZipReader(context.Background(), archive, size, limits)
	response := archiveWorkerResponse{Entries: entries}
	if err != nil {
		response = archiveWorkerResponse{ErrorCode: errorCode(err)}
	}
	return writeWorkerResponse(os.Stdout, response)
}

func runArchiveExtractWorker(archive *os.File, size int64, arguments []string) error {
	if len(arguments) != 3 {
		return writeWorkerResponse(os.Stdout, archiveWorkerResponse{ErrorCode: errorCode(ErrArchiveUnsafe)})
	}
	ordinal, err := strconv.Atoi(arguments[2])
	if err != nil || ordinal < 0 {
		return writeWorkerResponse(os.Stdout, archiveWorkerResponse{ErrorCode: errorCode(ErrArchiveUnsafe)})
	}
	return extractSevenZipReader(archive, size, ordinal, os.Stdout, DefaultArchiveLimits())
}

func ScanSevenZip(ctx context.Context, path string, limits ArchiveLimits) ([]ArchiveEntry, error) {
	workerContext, cancel := context.WithTimeout(ctx, archiveWorkerTimeout)
	defer cancel()
	response, err := runArchiveWorker(
		workerContext,
		path,
		"scan",
		strconv.Itoa(limits.MaxEntries),
		strconv.FormatInt(limits.MaxEntryBytes, 10),
		strconv.FormatInt(limits.MaxExpandedBytes, 10),
		strconv.FormatInt(limits.MaxCompressionRatio, 10),
		strconv.FormatBool(limits.AllowNestedArchives),
	)
	if err != nil {
		return nil, err
	}
	if response.ErrorCode != "" {
		return nil, archiveError(response.ErrorCode)
	}
	if len(response.Entries) > limits.MaxEntries {
		return nil, ErrArchiveLimitExceeded
	}
	return response.Entries, nil
}

func ExtractSevenZip(
	ctx context.Context,
	path string,
	expected ArchiveEntry,
	destination io.Writer,
) error {
	workerContext, cancel := context.WithTimeout(ctx, archiveWorkerTimeout)
	defer cancel()
	archive, err := openSevenZip(path)
	if err != nil {
		return fmt.Errorf("open 7z: %w", err)
	}
	defer func() { cleanup.Error("close", archive.Close()) }()
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("archive worker executable: %w", err)
	}
	command := exec.CommandContext(
		workerContext,
		executable,
		archiveWorkerCommand,
		"extract",
		strconv.Itoa(expected.Ordinal),
	)
	command.ExtraFiles = []*os.File{archive}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("archive worker stdout: %w", err)
	}
	if err := command.Start(); err != nil {
		return workerExecutionError(workerContext, err)
	}
	response, err := readWorkerResponse(stdout)
	if err != nil {
		_ = command.Process.Kill()
		waitErr := command.Wait()
		if waitErr != nil || workerContext.Err() != nil {
			return workerExecutionError(workerContext, errors.Join(err, waitErr))
		}
		return ErrArchiveUnsafe
	}
	if response.ErrorCode != "" {
		_ = command.Process.Kill()
		_ = command.Wait()
		return archiveError(response.ErrorCode)
	}
	if response.Size != expected.Size {
		_ = command.Process.Kill()
		_ = command.Wait()
		return ErrArchiveUnsafe
	}
	written, copyErr := io.CopyN(destination, stdout, response.Size)
	var trailing [1]byte
	trailingCount, trailingErr := stdout.Read(trailing[:])
	waitErr := command.Wait()
	if copyErr != nil || written != response.Size || trailingCount != 0 || !errors.Is(trailingErr, io.EOF) {
		return workerExecutionError(workerContext, errors.Join(copyErr, trailingErr, waitErr))
	}
	if waitErr != nil {
		return workerExecutionError(workerContext, waitErr)
	}
	return nil
}

func runArchiveWorker(ctx context.Context, path string, arguments ...string) (archiveWorkerResponse, error) {
	return runArchiveWorkerProcess(ctx, path, exec.CommandContext, arguments...)
}

func runArchiveWorkerProcess(
	ctx context.Context,
	path string,
	commandFactory archiveWorkerCommandFactory,
	arguments ...string,
) (archiveWorkerResponse, error) {
	archive, err := openSevenZip(path)
	if err != nil {
		return archiveWorkerResponse{}, fmt.Errorf("open 7z: %w", err)
	}
	defer func() { cleanup.Error("close", archive.Close()) }()
	executable, err := os.Executable()
	if err != nil {
		return archiveWorkerResponse{}, fmt.Errorf("archive worker executable: %w", err)
	}
	commandArguments := append([]string{archiveWorkerCommand}, arguments...)
	command := commandFactory(ctx, executable, commandArguments...)
	command.ExtraFiles = []*os.File{archive}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return archiveWorkerResponse{}, fmt.Errorf("archive worker stdout: %w", err)
	}
	if err := command.Start(); err != nil {
		return archiveWorkerResponse{}, workerExecutionError(ctx, err)
	}
	response, readErr := readWorkerResponse(stdout)
	if readErr != nil {
		_ = command.Process.Kill()
		waitErr := command.Wait()
		if waitErr != nil || ctx.Err() != nil {
			return archiveWorkerResponse{}, workerExecutionError(ctx, errors.Join(readErr, waitErr))
		}
		return archiveWorkerResponse{}, ErrArchiveUnsafe
	}
	var trailing [1]byte
	trailingCount, trailingErr := stdout.Read(trailing[:])
	waitErr := command.Wait()
	if trailingCount != 0 || !errors.Is(trailingErr, io.EOF) || waitErr != nil {
		return archiveWorkerResponse{}, workerExecutionError(ctx, errors.Join(trailingErr, waitErr))
	}
	return response, nil
}

func archiveLimitsFromArguments(arguments []string) (ArchiveLimits, error) {
	if len(arguments) != 5 {
		return ArchiveLimits{}, ErrArchiveUnsafe
	}
	maxEntries, err := strconv.Atoi(arguments[0])
	if err != nil {
		return ArchiveLimits{}, ErrArchiveUnsafe
	}
	maxEntryBytes, err := strconv.ParseInt(arguments[1], 10, 64)
	if err != nil {
		return ArchiveLimits{}, ErrArchiveUnsafe
	}
	maxExpandedBytes, err := strconv.ParseInt(arguments[2], 10, 64)
	if err != nil {
		return ArchiveLimits{}, ErrArchiveUnsafe
	}
	maxRatio, err := strconv.ParseInt(arguments[3], 10, 64)
	if err != nil {
		return ArchiveLimits{}, ErrArchiveUnsafe
	}
	allowNestedArchives, err := strconv.ParseBool(arguments[4])
	if err != nil {
		return ArchiveLimits{}, ErrArchiveUnsafe
	}
	defaults := DefaultArchiveLimits()
	if maxEntries < 0 || maxEntries > defaults.MaxEntries || maxEntryBytes < 0 ||
		maxEntryBytes > defaults.MaxEntryBytes || maxExpandedBytes < 0 ||
		maxExpandedBytes > defaults.MaxExpandedBytes || maxRatio < 0 || maxRatio > defaults.MaxCompressionRatio {
		return ArchiveLimits{}, ErrArchiveUnsafe
	}
	return ArchiveLimits{
		MaxEntries: maxEntries, MaxEntryBytes: maxEntryBytes,
		MaxExpandedBytes: maxExpandedBytes, MaxCompressionRatio: maxRatio,
		AllowNestedArchives: allowNestedArchives,
	}, nil
}

func openSevenZip(path string) (*os.File, error) {
	archive, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open 7z: %w", err)
	}
	var signature [6]byte
	if _, err := io.ReadFull(archive, signature[:]); err != nil || !bytes.Equal(signature[:], sevenZipMagic) {
		cleanup.Error("close", archive.Close())
		return nil, ErrArchiveUnsafe
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", archive.Close())
		return nil, ErrArchiveUnsafe
	}
	return archive, nil
}

func workerExecutionError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		if errors.Is(contextErr, context.DeadlineExceeded) {
			return ErrArchiveResourceLimit
		}
		return fmt.Errorf("archive worker: %w", contextErr)
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return ErrArchiveResourceLimit
	}
	return ErrArchiveUnsafe
}

// Every rejection is a security boundary of the archive contract.
func scanSevenZipReader(
	ctx context.Context,
	reader io.ReaderAt,
	archiveSize int64,
	limits ArchiveLimits,
) ([]ArchiveEntry, error) {
	archive, err := sevenzip.NewReader(reader, archiveSize)
	if err != nil {
		return nil, classifySevenZipReadError(err, false)
	}
	if len(archive.File) > limits.MaxEntries {
		return nil, ErrArchiveLimitExceeded
	}
	seenPath := make(map[string]struct{}, len(archive.File))
	seenFold := make(map[string]struct{}, len(archive.File))
	entries := make([]ArchiveEntry, 0, len(archive.File))
	var total int64
	for ordinal, item := range archive.File {
		entry, expanded, directory, err := scanSevenZipItem(
			ctx, item, ordinal, limits, total, seenPath, seenFold,
		)
		if err != nil {
			return nil, err
		}
		if directory {
			continue
		}
		total += expanded
		entries = append(entries, entry)
	}
	if archiveSize <= 0 && total > 0 || archiveSize > 0 && compressionRatioExceeded(
		total,
		archiveSize,
		limits.MaxCompressionRatio,
	) {
		return nil, ErrArchiveLimitExceeded
	}
	return entries, nil
}

func scanSevenZipItem(
	ctx context.Context,
	item *sevenzip.File,
	ordinal int,
	limits ArchiveLimits,
	total int64,
	seenPath, seenFold map[string]struct{},
) (ArchiveEntry, int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return ArchiveEntry{}, 0, false, fmt.Errorf("importing/sevenzip: %w", err)
	}
	pathValue, directory, err := archivePath(item.Name)
	if err != nil {
		return ArchiveEntry{}, 0, false, err
	}
	mode := item.Mode()
	if unsafeSevenZipMode(mode) {
		return ArchiveEntry{}, 0, false, ErrArchiveUnsafe
	}
	if directory || mode.IsDir() {
		return ArchiveEntry{}, 0, true, nil
	}
	expanded, err := sevenZipExpandedSize(item.UncompressedSize, limits, total)
	if err != nil {
		return ArchiveEntry{}, 0, false, err
	}
	folded := ASCIICaseFold(pathValue)
	if err := recordArchivePath(seenPath, seenFold, pathValue, folded); err != nil {
		return ArchiveEntry{}, 0, false, err
	}
	reader, err := item.Open()
	if err != nil {
		return ArchiveEntry{}, 0, false, classifySevenZipReadError(err, true)
	}
	entry, readErr := hashSevenZipEntry(
		ctx, reader, ordinal, pathValue, folded, expanded, item.CRC32, limits.AllowNestedArchives,
	)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return ArchiveEntry{}, 0, false,
			classifySevenZipReadError(errors.Join(readErr, closeErr), false)
	}
	return entry, expanded, false, nil
}

func unsafeSevenZipMode(mode os.FileMode) bool {
	return mode&os.ModeSymlink != 0 || mode&os.ModeType != 0 && !mode.IsDir()
}

func sevenZipExpandedSize(size uint64, limits ArchiveLimits, total int64) (int64, error) {
	if size > math.MaxInt64 {
		return 0, ErrArchiveLimitExceeded
	}
	expanded := int64(size)
	if limits.MaxEntryBytes < 0 || expanded > limits.MaxEntryBytes ||
		limits.MaxExpandedBytes < 0 || expanded > limits.MaxExpandedBytes-total {
		return 0, ErrArchiveLimitExceeded
	}
	return expanded, nil
}

func compressionRatioExceeded(expanded, compressed, maximum int64) bool {
	if expanded == 0 {
		return false
	}
	if compressed <= 0 || maximum < 0 || maximum == 0 {
		return true
	}
	if compressed > math.MaxInt64/maximum {
		return false
	}
	return expanded > compressed*maximum
}

func hashSevenZipEntry(
	ctx context.Context,
	reader io.Reader,
	ordinal int,
	pathValue, folded string,
	expectedSize int64,
	expectedCRC32 uint32,
	allowNestedArchives bool,
) (ArchiveEntry, error) {
	sha256Hash := sha256.New()
	legacyHashes := legacychecksum.New()
	crc32Hash := crc32.NewIEEE()
	prefix := make([]byte, 512)
	buffer := make([]byte, 1024*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return ArchiveEntry{}, fmt.Errorf("importing/sevenzip: %w", err)
		}
		count, err := reader.Read(buffer)
		if count > 0 {
			if written+int64(count) > expectedSize {
				return ArchiveEntry{}, ErrArchiveUnsafe
			}
			if written < int64(len(prefix)) {
				copy(prefix[written:], buffer[:count])
			}
			_, _ = io.MultiWriter(sha256Hash, legacyHashes.MD5, legacyHashes.SHA1, crc32Hash).Write(buffer[:count])
			written += int64(count)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ArchiveEntry{}, fmt.Errorf("read 7z entry: %w", err)
		}
	}
	if written != expectedSize || crc32Hash.Sum32() != expectedCRC32 {
		return ArchiveEntry{}, ErrArchiveUnsafe
	}
	nestedFormat := DetectNestedArchive(pathValue, prefix[:min(int64(len(prefix)), written)])
	if !allowNestedArchives && nestedFormat != NestedArchiveNone {
		return ArchiveEntry{}, ErrNestedArchiveUnsupported
	}
	return ArchiveEntry{
		Ordinal: ordinal, OriginalPath: pathValue, NormalizedPath: pathValue, ASCIICasefoldPath: folded,
		ArchiveFormat: "SEVEN_Z", CompressionProfile: "SEVEN_Z_DECODER_VALIDATED", Size: written,
		CRC32: hex.EncodeToString(crc32Hash.Sum(nil)), MD5: hex.EncodeToString(legacyHashes.MD5.Sum(nil)),
		SHA1: hex.EncodeToString(legacyHashes.SHA1.Sum(nil)), SHA256: hex.EncodeToString(sha256Hash.Sum(nil)),
		NestedArchive: nestedFormat,
	}, nil
}

func extractSevenZipReader(
	reader io.ReaderAt,
	archiveSize int64,
	ordinal int,
	destination io.Writer,
	limits ArchiveLimits,
) error {
	archive, err := sevenzip.NewReader(reader, archiveSize)
	if err != nil {
		return writeWorkerResponse(destination, archiveWorkerResponse{
			ErrorCode: errorCode(classifySevenZipReadError(err, false)),
		})
	}
	if ordinal >= len(archive.File) {
		return writeWorkerResponse(destination, archiveWorkerResponse{ErrorCode: errorCode(ErrArchiveUnsafe)})
	}
	item := archive.File[ordinal]
	if item.UncompressedSize > math.MaxInt64 || int64(item.UncompressedSize) > limits.MaxEntryBytes {
		return writeWorkerResponse(destination, archiveWorkerResponse{ErrorCode: errorCode(ErrArchiveLimitExceeded)})
	}
	entryReader, err := item.Open()
	if err != nil {
		return writeWorkerResponse(destination, archiveWorkerResponse{
			ErrorCode: errorCode(classifySevenZipReadError(err, true)),
		})
	}
	defer func() { cleanup.Error("close", entryReader.Close()) }()
	if err := writeWorkerResponse(destination, archiveWorkerResponse{Size: int64(item.UncompressedSize)}); err != nil {
		return err
	}
	written, err := io.CopyN(destination, entryReader, int64(item.UncompressedSize))
	if err != nil || written != int64(item.UncompressedSize) {
		return ErrArchiveUnsafe
	}
	return nil
}

func classifySevenZipReadError(err error, openingEntry bool) error {
	var readError *sevenzip.ReadError
	if errors.As(err, &readError) && readError.Encrypted {
		return ErrArchiveEncrypted
	}
	if openingEntry && errors.As(err, &readError) {
		return ErrArchiveMethodUnsupported
	}
	if errors.Is(err, ErrNestedArchiveUnsupported) {
		return ErrNestedArchiveUnsupported
	}
	return ErrArchiveUnsafe
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrArchiveEncrypted):
		return ErrArchiveEncrypted.Error()
	case errors.Is(err, ErrArchiveVolumeUnsupported):
		return ErrArchiveVolumeUnsupported.Error()
	case errors.Is(err, ErrArchiveResourceLimit):
		return ErrArchiveResourceLimit.Error()
	case errors.Is(err, ErrArchiveSandboxUnavailable):
		return ErrArchiveSandboxUnavailable.Error()
	case errors.Is(err, ErrArchiveLimitExceeded):
		return ErrArchiveLimitExceeded.Error()
	case errors.Is(err, ErrNestedArchiveUnsupported):
		return ErrNestedArchiveUnsupported.Error()
	case errors.Is(err, ErrArchiveMethodUnsupported):
		return ErrArchiveMethodUnsupported.Error()
	case errors.Is(err, ErrArchiveCasefoldCollision):
		return ErrArchiveCasefoldCollision.Error()
	default:
		return ErrArchiveUnsafe.Error()
	}
}

func archiveError(code string) error {
	switch code {
	case "ARCHIVE_ENCRYPTED_UNSUPPORTED":
		return ErrArchiveEncrypted
	case "ARCHIVE_VOLUME_UNSUPPORTED":
		return ErrArchiveVolumeUnsupported
	case "ARCHIVE_RESOURCE_LIMIT":
		return ErrArchiveResourceLimit
	case "ARCHIVE_SANDBOX_UNAVAILABLE":
		return ErrArchiveSandboxUnavailable
	case "ARCHIVE_LIMIT_EXCEEDED":
		return ErrArchiveLimitExceeded
	case "NESTED_ARCHIVE_UNSUPPORTED":
		return ErrNestedArchiveUnsupported
	case "ARCHIVE_METHOD_UNSUPPORTED":
		return ErrArchiveMethodUnsupported
	case "ARCHIVE_CASEFOLD_COLLISION":
		return ErrArchiveCasefoldCollision
	default:
		return ErrArchiveUnsafe
	}
}

func writeWorkerResponse(writer io.Writer, response archiveWorkerResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal archive response: %w", err)
	}
	if len(payload) > maxWorkerHeaderBytes {
		return ErrArchiveLimitExceeded
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	if _, err := writer.Write(length[:]); err != nil {
		return fmt.Errorf("write archive response length: %w", err)
	}
	if _, err := writer.Write(payload); err != nil {
		return fmt.Errorf("write archive response: %w", err)
	}
	return nil
}

func readWorkerResponse(reader io.Reader) (archiveWorkerResponse, error) {
	var length [4]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil {
		return archiveWorkerResponse{}, fmt.Errorf("read archive response length: %w", err)
	}
	size := binary.BigEndian.Uint32(length[:])
	if size == 0 || size > maxWorkerHeaderBytes {
		return archiveWorkerResponse{}, ErrArchiveUnsafe
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return archiveWorkerResponse{}, fmt.Errorf("read archive response: %w", err)
	}
	var response archiveWorkerResponse
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return archiveWorkerResponse{}, fmt.Errorf("decode archive response: %w", err)
	}
	return response, nil
}

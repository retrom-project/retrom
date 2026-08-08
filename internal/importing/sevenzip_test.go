package importing

import (
	"bytes"
	"context"
	"crypto/md5"  //nolint:gosec // The archive contract records legacy checksums.
	"crypto/sha1" //nolint:gosec // The archive contract records legacy checksums.
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	handled, err := RunArchiveWorker(os.Args[1:])
	if handled {
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSevenZipWorkerScansAndExtractsDeterministicArchive(t *testing.T) {
	t.Parallel()
	archivePath := filepath.Join("testdata", "sevenzip", "single.7z")
	payload, err := os.ReadFile(filepath.Join("testdata", "sevenzip", "payload", "game.a26"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ScanSevenZip(context.Background(), archivePath, DefaultArchiveLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d", len(entries))
	}
	entry := entries[0]
	sha256Digest := sha256.Sum256(payload)
	md5Digest := md5.Sum(payload)   //nolint:gosec // The archive contract records legacy checksums.
	sha1Digest := sha1.Sum(payload) //nolint:gosec // The archive contract records legacy checksums.
	if entry.Ordinal != 0 || entry.NormalizedPath != "game.a26" || entry.Size != int64(len(payload)) ||
		entry.ArchiveFormat != "SEVEN_Z" || entry.CompressionProfile != "SEVEN_Z_DECODER_VALIDATED" ||
		entry.MD5 != hex.EncodeToString(md5Digest[:]) || entry.SHA1 != hex.EncodeToString(sha1Digest[:]) ||
		entry.SHA256 != hex.EncodeToString(sha256Digest[:]) {
		t.Fatalf("entry = %#v", entry)
	}
	if entry.CRC32 != crc32Hex(payload) {
		t.Fatalf("CRC32 = %s", entry.CRC32)
	}
	var extracted bytes.Buffer
	if err := ExtractSevenZip(context.Background(), archivePath, entry, &extracted); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(extracted.Bytes(), payload) {
		t.Fatalf("extracted payload = %q", extracted.Bytes())
	}
}

func TestSevenZipWorkerRejectsUnsupportedContainers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want error
	}{
		{name: "encrypted", path: "encrypted.7z", want: ErrArchiveEncrypted},
		{name: "nested", path: "nested.7z", want: ErrNestedArchiveUnsupported},
		{name: "casefold-collision", path: "casefold.7z", want: ErrArchiveCasefoldCollision},
		{name: "symlink", path: "symlink.7z", want: ErrArchiveUnsafe},
		{name: "unsupported-coder", path: "unsupported-coder.7z", want: ErrArchiveMethodUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ScanSevenZip(
				context.Background(),
				filepath.Join("testdata", "sevenzip", test.path),
				DefaultArchiveLimits(),
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("ScanSevenZip() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSevenZipPathValidationRejectsTraversalAndInvalidNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"../game.a26", "/game.a26", "folder//game.a26", "C:/game.a26", "game\\name.a26"} {
		if _, _, err := archivePath(name); err == nil {
			t.Errorf("archivePath(%q) accepted", name)
		}
	}
}

func TestSevenZipWorkerRejectsSFXCorruptionAndLimits(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "sevenzip", "single.7z"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		bytes  []byte
		limits ArchiveLimits
		want   error
	}{
		{name: "sfx-prefix", bytes: append([]byte("MZ"), source...), limits: DefaultArchiveLimits(), want: ErrArchiveUnsafe},
		{name: "damaged-header", bytes: corruptArchive(source), limits: DefaultArchiveLimits(), want: ErrArchiveUnsafe},
		{name: "entry-limit", bytes: source, limits: ArchiveLimits{MaxEntries: 0, MaxEntryBytes: 1 << 20, MaxExpandedBytes: 1 << 20, MaxCompressionRatio: 200}, want: ErrArchiveLimitExceeded},
		{name: "ratio-limit", bytes: source, limits: ArchiveLimits{MaxEntries: 10, MaxEntryBytes: 1 << 20, MaxExpandedBytes: 1 << 20, MaxCompressionRatio: 0}, want: ErrArchiveLimitExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "input.7z")
			if err := os.WriteFile(path, test.bytes, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := ScanSevenZip(context.Background(), path, test.limits)
			if !errors.Is(err, test.want) {
				t.Fatalf("ScanSevenZip() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestArchiveWorkerProtocolRejectsOversizeAndUnknownFields(t *testing.T) {
	t.Parallel()
	var oversized [4]byte
	binary.BigEndian.PutUint32(oversized[:], maxWorkerHeaderBytes+1)
	if _, err := readWorkerResponse(bytes.NewReader(oversized[:])); !errors.Is(err, ErrArchiveUnsafe) {
		t.Fatalf("oversized response error = %v", err)
	}
	payload := []byte(`{"entries":[],"unknown":true}`)
	var framed bytes.Buffer
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(payload))) //nolint:gosec // The test payload is a fixed short literal.
	framed.Write(size[:])
	framed.Write(payload)
	if _, err := readWorkerResponse(&framed); err == nil {
		t.Fatal("unknown response field was accepted")
	}
}

func TestSevenZipEntryChecksumMismatchIsUnsafeNotNested(t *testing.T) {
	t.Parallel()
	payload := []byte("not an archive")
	_, err := hashSevenZipEntry(
		context.Background(),
		bytes.NewReader(payload),
		0,
		"game.a26",
		"game.a26",
		int64(len(payload)),
		crc32.ChecksumIEEE(payload)+1,
	)
	if !errors.Is(err, ErrArchiveUnsafe) || errors.Is(err, ErrNestedArchiveUnsupported) {
		t.Fatalf("hashSevenZipEntry() error = %v", err)
	}
}

func TestArchiveWorkerFailuresHaveStableClassification(t *testing.T) {
	t.Parallel()
	deadlineContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := workerExecutionError(deadlineContext, context.DeadlineExceeded); !errors.Is(err, ErrArchiveResourceLimit) {
		t.Fatalf("deadline error = %v", err)
	}
	if err := workerExecutionError(context.Background(), &exec.ExitError{}); !errors.Is(err, ErrArchiveResourceLimit) {
		t.Fatalf("exit error = %v", err)
	}
	if err := workerExecutionError(context.Background(), errors.New("invalid IPC")); !errors.Is(err, ErrArchiveUnsafe) {
		t.Fatalf("protocol error = %v", err)
	}
}

func TestArchiveWorkerProcessFaultInjection(t *testing.T) {
	archivePath := filepath.Join("testdata", "sevenzip", "single.7z")
	tests := []struct {
		name    string
		mode    string
		timeout time.Duration
		want    error
	}{
		{name: "abnormal-exit", mode: "exit", timeout: time.Second, want: ErrArchiveResourceLimit},
		{name: "wall-timeout", mode: "sleep", timeout: 20 * time.Millisecond, want: ErrArchiveResourceLimit},
		{name: "malformed-ipc", mode: "malformed", timeout: time.Second, want: ErrArchiveUnsafe},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), test.timeout)
			defer cancel()
			factory := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestArchiveWorkerFakeProcess") //nolint:gosec // The current test binary is the intentional fake worker.
				command.Env = append(os.Environ(), "RETROM_FAKE_ARCHIVE_WORKER=1", "RETROM_FAKE_ARCHIVE_MODE="+test.mode)
				return command
			}
			_, err := runArchiveWorkerProcess(ctx, archivePath, factory, "scan", "1", "1", "1", "1")
			if !errors.Is(err, test.want) {
				t.Fatalf("runArchiveWorkerProcess() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestArchiveWorkerFakeProcess(_ *testing.T) {
	if os.Getenv("RETROM_FAKE_ARCHIVE_WORKER") != "1" {
		return
	}
	switch os.Getenv("RETROM_FAKE_ARCHIVE_MODE") {
	case "exit":
		os.Exit(9)
	case "sleep":
		time.Sleep(time.Hour)
	case "malformed":
		_, _ = os.Stdout.Write([]byte{0, 0, 0, 2, '{'})
		os.Exit(0)
	default:
		os.Exit(10)
	}
}

func crc32Hex(payload []byte) string {
	checksum := crc32.NewIEEE()
	_, _ = checksum.Write(payload)
	return hex.EncodeToString(checksum.Sum(nil))
}

func corruptArchive(source []byte) []byte {
	corrupted := append([]byte(nil), source...)
	corrupted[len(corrupted)-1] ^= 0xff
	return corrupted
}

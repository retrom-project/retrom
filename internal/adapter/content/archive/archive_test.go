package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"testing"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
	"retrom/internal/testkit/testassert"
	"retrom/internal/testkit/testsupport"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func writeZIP(t *testing.T, name string, contents []byte) string {
	t.Helper()
	path := t.TempDir() + "/fixture.zip"
	file, err := os.Create(path)
	testassert.False(t, err != nil, err)
	archive := zip.NewWriter(file)
	entry, err := archive.Create(name)
	if err == nil {
		_, err = entry.Write(contents)
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	testassert.False(t, err != nil, err)
	return path
}

func writeLegacyNamedZIP(t *testing.T, name string) string {
	t.Helper()
	encoded, err := simplifiedchinese.GB18030.NewEncoder().String(name)
	testassert.False(t, err != nil, err)
	path := t.TempDir() + "/legacy.zip"
	file, err := os.Create(path)
	testassert.False(t, err != nil, err)
	archive := zip.NewWriter(file)
	header := &zip.FileHeader{Name: encoded, NonUTF8: true, Method: zip.Deflate}
	entry, err := archive.CreateHeader(header)
	if err == nil {
		_, err = entry.Write([]byte("fixture-rom"))
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	testassert.False(t, err != nil, err)
	return path
}

func writeEncryptedFlagZIP(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/encrypted.zip"
	file, err := os.Create(path)
	testassert.False(t, err != nil, err)
	archive := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "data.xp3", Method: zip.Store, Flags: 0x1}
	entry, err := archive.CreateRaw(header)
	if err == nil {
		_, err = entry.Write([]byte("encrypted fixture marker"))
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	testassert.False(t, err != nil, err)
	return path
}

func TestScanZIPDecodesLegacyGB18030EntryName(t *testing.T) {
	t.Parallel()
	entries, err := New(&testsupport.DiagnosticRecorder{}).ScanZIP(
		context.Background(),
		writeLegacyNamedZIP(t, "RPG制造.gba"),
		importing.DefaultArchiveLimits(),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(entries) != 1 }, func() bool { return entries[0].NormalizedPath != "RPG制造.gba" }), "ScanZIP() = %#v, error=%v", entries, err)
}

func TestScanZIPValidatesDecodedLegacyPath(t *testing.T) {
	t.Parallel()
	_, err := New(&testsupport.DiagnosticRecorder{}).ScanZIP(
		context.Background(),
		writeLegacyNamedZIP(t, "目录/../game.gba"),
		importing.DefaultArchiveLimits(),
	)
	testassert.Truef(t, errors.Is(err, importing.ErrUnsafeLogicalPath), "ScanZIP() error = %v, want %v", err, importing.ErrUnsafeLogicalPath)
}

func TestScanZIPReportsEncryptedEntriesPrecisely(t *testing.T) {
	t.Parallel()
	_, err := New(&testsupport.DiagnosticRecorder{}).ScanZIP(context.Background(), writeEncryptedFlagZIP(t), importing.RPGMakerArchiveLimits())
	testassert.Truef(t, errors.Is(err, importing.ErrArchiveEncrypted), "ScanZIP() error = %v, want %v", err, importing.ErrArchiveEncrypted)
}

func TestScanZIPWithConsumerStreamsEachEntryOnce(t *testing.T) {
	t.Parallel()
	body := []byte("stream directly into content-addressed storage")
	calls := 0
	entries, err := consumeProject(context.Background(), t, contentprofile.ArchiveZIP, writeZIP(t, "project/data.bin", body), importing.RPGMakerArchiveLimits(),
		func(header importing.ArchiveEntry, reader io.Reader) (importing.ArchiveContent, error) {
			calls++
			if header.Ordinal != 0 || header.NormalizedPath != "project/data.bin" {
				t.Fatalf("consumer header = %#v", header)
			}
			contents, readErr := io.ReadAll(reader)
			if readErr != nil {
				return importing.ArchiveContent{}, readErr
			}
			sha256Digest := sha256.Sum256(contents)
			md5Digest := md5.Sum(contents)
			sha1Digest := sha1.Sum(contents)
			return importing.ArchiveContent{
				Size: int64(len(contents)), CRC32: fmt.Sprintf("%08x", crc32.ChecksumIEEE(contents)),
				MD5: hex.EncodeToString(md5Digest[:]), SHA1: hex.EncodeToString(sha1Digest[:]),
				SHA256: hex.EncodeToString(sha256Digest[:]),
			}, nil
		},
	)
	if err != nil || calls != 1 || len(entries) != 1 || entries[0].SHA256 == "" {
		t.Fatalf("ZIP project cursor entries=%#v calls=%d error=%v", entries, calls, err)
	}
}

func TestDOSArchiveLimitsAllowBoundedSparseSavesAndOpaqueNestedData(t *testing.T) {
	t.Parallel()
	sparse := writeZIP(t, "GAME/EMPTY.SAV", bytes.Repeat([]byte{0}, 1<<20))
	if _, err := New(&testsupport.DiagnosticRecorder{}).ScanZIP(context.Background(), sparse, importing.DefaultArchiveLimits()); err != nil {
		t.Fatalf("bounded sparse save rejected: %v", err)
	}
	nested := writeZIP(t, "GAME/DOSBOX/runtime.zip", []byte("PK\x03\x04nested payload"))
	if _, err := New(&testsupport.DiagnosticRecorder{}).ScanZIP(context.Background(), nested, importing.DefaultArchiveLimits()); !errors.Is(err, importing.ErrNestedArchiveUnsupported) {
		t.Fatalf("default nested archive error = %v", err)
	}
	entries, err := New(&testsupport.DiagnosticRecorder{}).ScanZIP(context.Background(), nested, importing.DOSArchiveLimits())
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(entries) != 1 }, func() bool { return entries[0].NormalizedPath != "GAME/DOSBOX/runtime.zip" }), "DOS opaque nested data = %#v, error=%v", entries, err)
}

func TestRPGMakerArchiveLimitsClassifyNestedDataWithoutRelaxingDefault(t *testing.T) {
	t.Parallel()
	nested := writeZIP(t, "www/audio/bgm/config", []byte("7z\xbc\xaf\x27\x1cnested payload"))
	if _, err := New(&testsupport.DiagnosticRecorder{}).ScanZIP(context.Background(), nested, importing.DefaultArchiveLimits()); !errors.Is(err, importing.ErrNestedArchiveUnsupported) {
		t.Fatalf("default nested archive error = %v", err)
	}
	entries, err := New(&testsupport.DiagnosticRecorder{}).ScanZIP(context.Background(), nested, importing.RPGMakerArchiveLimits())
	if err != nil || len(entries) != 1 || entries[0].NestedArchive != importing.NestedArchiveSevenZip {
		t.Fatalf("RPG Maker nested classification = %#v, error=%v", entries, err)
	}
}

func TestZIPCompressionRatioStillRejectsLargeHighlyCompressedMembers(t *testing.T) {
	t.Parallel()
	large := writeZIP(t, "GAME/large.bin", bytes.Repeat([]byte{0}, (16<<20)+1))
	if _, err := New(&testsupport.DiagnosticRecorder{}).ScanZIP(context.Background(), large, importing.DefaultArchiveLimits()); !errors.Is(err, importing.ErrArchiveLimitExceeded) {
		t.Fatalf("large high-ratio member error = %v", err)
	}
}

func TestScanFlatZIPRejectsDirectoriesAndSubdirectories(t *testing.T) {
	t.Parallel()
	if entries, err := New(&testsupport.DiagnosticRecorder{}).ScanFlatZIP(
		context.Background(), writeZIP(t, "a.bin", []byte("a")), importing.DefaultArchiveLimits(),
	); err != nil || len(entries) != 1 {
		t.Fatalf("flat entries = %d, error=%v", len(entries), err)
	}
	if _, err := New(&testsupport.DiagnosticRecorder{}).ScanFlatZIP(
		context.Background(), writeZIP(t, "dir/a.bin", []byte("a")), importing.DefaultArchiveLimits(),
	); !errors.Is(err, importing.ErrNestedArchiveUnsupported) {
		t.Fatalf("nested path error = %v", err)
	}
	if _, err := New(&testsupport.DiagnosticRecorder{}).ScanFlatZIP(
		context.Background(), writeZIP(t, "dir/", nil), importing.DefaultArchiveLimits(),
	); !errors.Is(err, importing.ErrNestedArchiveUnsupported) {
		t.Fatalf("directory error = %v", err)
	}
}

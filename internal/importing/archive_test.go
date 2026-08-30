package importing

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

	"retrom/internal/testassert"

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
	entries, err := ScanZIP(
		context.Background(),
		writeLegacyNamedZIP(t, "RPG制造.gba"),
		DefaultArchiveLimits(),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(entries) != 1 }, func() bool { return entries[0].NormalizedPath != "RPG制造.gba" }), "ScanZIP() = %#v, error=%v", entries, err)
}

func TestScanZIPValidatesDecodedLegacyPath(t *testing.T) {
	t.Parallel()
	_, err := ScanZIP(
		context.Background(),
		writeLegacyNamedZIP(t, "目录/../game.gba"),
		DefaultArchiveLimits(),
	)
	testassert.Truef(t, errors.Is(err, ErrUnsafeLogicalPath), "ScanZIP() error = %v, want %v", err, ErrUnsafeLogicalPath)
}

func TestScanZIPReportsEncryptedEntriesPrecisely(t *testing.T) {
	t.Parallel()
	_, err := ScanZIP(context.Background(), writeEncryptedFlagZIP(t), RPGMakerArchiveLimits())
	testassert.Truef(t, errors.Is(err, ErrArchiveEncrypted), "ScanZIP() error = %v, want %v", err, ErrArchiveEncrypted)
}

func TestScanZIPWithConsumerStreamsEachEntryOnce(t *testing.T) {
	t.Parallel()
	body := []byte("stream directly into content-addressed storage")
	calls := 0
	entries, err := ScanZIPWithConsumer(
		context.Background(), writeZIP(t, "project/data.bin", body), RPGMakerArchiveLimits(),
		func(header ArchiveEntry, reader io.Reader) (ArchiveContent, error) {
			calls++
			if header.Ordinal != 0 || header.NormalizedPath != "project/data.bin" {
				t.Fatalf("consumer header = %#v", header)
			}
			contents, readErr := io.ReadAll(reader)
			if readErr != nil {
				return ArchiveContent{}, readErr
			}
			sha256Digest := sha256.Sum256(contents)
			md5Digest := md5.Sum(contents)
			sha1Digest := sha1.Sum(contents)
			return ArchiveContent{
				Size: int64(len(contents)), CRC32: fmt.Sprintf("%08x", crc32.ChecksumIEEE(contents)),
				MD5: hex.EncodeToString(md5Digest[:]), SHA1: hex.EncodeToString(sha1Digest[:]),
				SHA256: hex.EncodeToString(sha256Digest[:]),
			}, nil
		},
	)
	if err != nil || calls != 1 || len(entries) != 1 || entries[0].SHA256 == "" {
		t.Fatalf("ScanZIPWithConsumer() entries=%#v calls=%d error=%v", entries, calls, err)
	}
}

func TestDOSArchiveLimitsAllowBoundedSparseSavesAndOpaqueNestedData(t *testing.T) {
	t.Parallel()
	sparse := writeZIP(t, "GAME/EMPTY.SAV", bytes.Repeat([]byte{0}, 1<<20))
	if _, err := ScanZIP(context.Background(), sparse, DefaultArchiveLimits()); err != nil {
		t.Fatalf("bounded sparse save rejected: %v", err)
	}
	nested := writeZIP(t, "GAME/DOSBOX/runtime.zip", []byte("PK\x03\x04nested payload"))
	if _, err := ScanZIP(context.Background(), nested, DefaultArchiveLimits()); !errors.Is(err, ErrNestedArchiveUnsupported) {
		t.Fatalf("default nested archive error = %v", err)
	}
	entries, err := ScanZIP(context.Background(), nested, DOSArchiveLimits())
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(entries) != 1 }, func() bool { return entries[0].NormalizedPath != "GAME/DOSBOX/runtime.zip" }), "DOS opaque nested data = %#v, error=%v", entries, err)
}

func TestRPGMakerArchiveLimitsClassifyNestedDataWithoutRelaxingDefault(t *testing.T) {
	t.Parallel()
	nested := writeZIP(t, "www/audio/bgm/config", []byte("7z\xbc\xaf\x27\x1cnested payload"))
	if _, err := ScanZIP(context.Background(), nested, DefaultArchiveLimits()); !errors.Is(err, ErrNestedArchiveUnsupported) {
		t.Fatalf("default nested archive error = %v", err)
	}
	entries, err := ScanZIP(context.Background(), nested, RPGMakerArchiveLimits())
	if err != nil || len(entries) != 1 || entries[0].NestedArchive != NestedArchiveSevenZip {
		t.Fatalf("RPG Maker nested classification = %#v, error=%v", entries, err)
	}
}

func TestZIPCompressionRatioStillRejectsLargeHighlyCompressedMembers(t *testing.T) {
	t.Parallel()
	large := writeZIP(t, "GAME/large.bin", bytes.Repeat([]byte{0}, (16<<20)+1))
	if _, err := ScanZIP(context.Background(), large, DefaultArchiveLimits()); !errors.Is(err, ErrArchiveLimitExceeded) {
		t.Fatalf("large high-ratio member error = %v", err)
	}
}

func TestScanFlatZIPRejectsDirectoriesAndSubdirectories(t *testing.T) {
	t.Parallel()
	if entries, err := ScanFlatZIP(
		context.Background(), writeZIP(t, "a.bin", []byte("a")), DefaultArchiveLimits(),
	); err != nil || len(entries) != 1 {
		t.Fatalf("flat entries = %d, error=%v", len(entries), err)
	}
	if _, err := ScanFlatZIP(
		context.Background(), writeZIP(t, "dir/a.bin", []byte("a")), DefaultArchiveLimits(),
	); !errors.Is(err, ErrNestedArchiveUnsupported) {
		t.Fatalf("nested path error = %v", err)
	}
	if _, err := ScanFlatZIP(
		context.Background(), writeZIP(t, "dir/", nil), DefaultArchiveLimits(),
	); !errors.Is(err, ErrNestedArchiveUnsupported) {
		t.Fatalf("directory error = %v", err)
	}
}

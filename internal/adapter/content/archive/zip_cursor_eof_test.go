package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/format/importing"
	"retrom/internal/testkit/testsupport"
)

func TestZIPCompleteValidatesPendingDataDescriptor(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		body    []byte
		corrupt bool
	}{
		{name: "valid body", body: []byte("terminal CRC is still pending")},
		{name: "corrupt body", body: []byte("terminal CRC is still pending"), corrupt: true},
		{name: "valid empty"},
		{name: "corrupt empty", corrupt: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkZIPDescriptorCompletion(t, test.body, test.corrupt)
		})
	}
}

func checkZIPDescriptorCompletion(t *testing.T, body []byte, corrupt bool) {
	t.Helper()
	path := writeZIPWithDescriptor(t, body, corrupt)
	assertZIPDescriptorRead(t, path, body, corrupt)
	cursor, member, outer, events := openObservedDescriptorCursor(t.Context(), t, path)
	got := make([]byte, len(body))
	if _, err := io.ReadFull(cursor, got); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("exact body read=%q err=%v", got, err)
	}
	reads := member.reads
	entry, completeErr := cursor.Complete(testArchiveContent(got))
	if member.reads != reads+1 {
		t.Fatalf("terminal validation reads=%d, want one after %d", member.reads, reads)
	}
	_, nextErr := cursor.Next()
	if corrupt {
		if !errors.Is(completeErr, zip.ErrChecksum) || !errors.Is(nextErr, zip.ErrChecksum) {
			t.Fatalf("corrupt descriptor Complete=%v Next=%v", completeErr, nextErr)
		}
		if entry != (importing.ArchiveEntry{}) {
			t.Fatalf("corrupt descriptor produced entry=%+v", entry)
		}
	} else if completeErr != nil || entry.Size != int64(len(body)) || !sameErrorObject(nextErr, io.EOF) {
		t.Fatalf("valid descriptor entry=%+v Complete=%v Next=%v", entry, completeErr, nextErr)
	}
	for range 2 {
		if err := cursor.Close(); err != nil {
			t.Fatal(err)
		}
	}
	assertCloseEvents(t, *events, []string{"member", "archive"})
	if member.closer.calls != 1 || outer.calls != 1 {
		t.Fatalf("resource Close calls: member=%d archive=%d", member.closer.calls, outer.calls)
	}
}

func writeZIPWithDescriptor(t *testing.T, body []byte, corrupt bool) string {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	member, err := writer.CreateHeader(&zip.FileHeader{Name: "data.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	raw := data.Bytes()
	offset := bytes.Index(raw, []byte{0x50, 0x4b, 0x07, 0x08})
	if offset < 0 || offset+16 > len(raw) {
		t.Fatal("fixture lacks a data descriptor")
	}
	if corrupt {
		crc := binary.LittleEndian.Uint32(raw[offset+4 : offset+8])
		binary.LittleEndian.PutUint32(raw[offset+4:offset+8], crc^1)
	}
	path := filepath.Join(t.TempDir(), "descriptor.zip")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertZIPDescriptorRead(t *testing.T, path string, body []byte, corrupt bool) {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	member, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(member)
	closeErr := member.Close()
	if !bytes.Equal(got, body) || closeErr != nil {
		t.Fatalf("fixture bytes=%q close=%v", got, closeErr)
	}
	if corrupt && !errors.Is(readErr, zip.ErrChecksum) || !corrupt && readErr != nil {
		t.Fatalf("fixture corrupt=%v read=%v", corrupt, readErr)
	}
}

func openObservedDescriptorCursor(
	ctx context.Context, t *testing.T, path string,
) (*zipCursor, *observedReader, *observedCloser, *[]string) {
	t.Helper()
	cursor, err := New(&testsupport.DiagnosticRecorder{}).openZIP(ctx, path, importing.DefaultArchiveLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cursor.Close(); err != nil {
			t.Error(err)
		}
	})
	events := make([]string, 0, 2)
	outer := &observedCloser{name: "archive", events: &events, resource: cursor.archive.file}
	cursor.archive.file = outer
	if _, err := cursor.Next(); err != nil {
		t.Fatal(err)
	}
	member := &observedReader{
		reader: cursor.active,
		closer: observedCloser{name: "member", events: &events, resource: cursor.active},
	}
	cursor.active = member
	cursor.monitor.reader = member
	return cursor, member, outer, &events
}

func TestZIPCompleteDoesNotRereadStagedMemberAfterCancellation(t *testing.T) {
	t.Parallel()
	body := []byte("fully staged member")
	path := writeZIPWithDescriptor(t, body, false)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	cursor, member, _, _ := openObservedDescriptorCursor(ctx, t, path)
	store, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.Stage(cursor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := candidate.Discard(); err != nil {
			t.Error(err)
		}
	})
	reads := member.reads
	cancel()
	metadata := candidate.Metadata()
	entry, err := cursor.Complete(importing.ArchiveContent{
		Size: metadata.Size, CRC32: metadata.CRC32, MD5: metadata.MD5,
		SHA1: metadata.SHA1, SHA256: metadata.SHA256,
	})
	if err != nil || entry.Size != int64(len(body)) || member.reads != reads {
		t.Fatalf("staged Complete=%+v err=%v reads=%d want=%d", entry, err, member.reads, reads)
	}
}

func TestZIPCompletePreservesPureFailureBeforePendingEOF(t *testing.T) {
	t.Parallel()
	body := []byte{'P', 'K', 3, 4, 'd', 'a', 't', 'a'}
	for _, rule := range []string{"size", "crc", "nested"} {
		t.Run(rule, func(t *testing.T) {
			t.Parallel()
			path := writeZIPWithDescriptor(t, body, true)
			cursor, member, _, _ := openObservedDescriptorCursor(t.Context(), t, path)
			read := make([]byte, len(body))
			if _, err := io.ReadFull(cursor, read); err != nil {
				t.Fatal(err)
			}
			content := testArchiveContent(read)
			want := importing.ErrArchiveUnsafe
			switch rule {
			case "size":
				content.Size++
			case "crc":
				content.CRC32 = "00000000"
			case "nested":
				want = importing.ErrNestedArchiveUnsupported
			}
			reads := member.reads
			_, err := cursor.Complete(content)
			if !errors.Is(err, want) || member.reads != reads {
				t.Fatalf("rule=%s error=%v reads=%d want=%d", rule, err, member.reads, reads)
			}
		})
	}
}

func TestZIPCompleteRetainsEarlierReadFailure(t *testing.T) {
	t.Parallel()
	body := []byte("read failure wins over pending EOF")
	cause := errors.New("member read failed")
	cursor, member, _, _, _ := faultZIPCursor(t, body)
	member.reader = &oneReadFault{contents: body, failure: cause}
	if _, err := cursor.Next(); err != nil {
		t.Fatal(err)
	}
	count, readErr := cursor.Read(make([]byte, len(body)+1))
	if count != len(body) || !errors.Is(readErr, cause) {
		t.Fatalf("initial read=%d error=%v", count, readErr)
	}
	reads := member.reads
	_, completeErr := cursor.Complete(testArchiveContent(body))
	if !sameErrorObject(errors.Unwrap(completeErr), readErr) || member.reads != reads {
		t.Fatalf("Complete changed earlier error=%v or reads=%d want=%d", completeErr, member.reads, reads)
	}
}

package dosbundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestOverlayPrependsDeterministicLauncherAndPreservesArchive(t *testing.T) {
	t.Parallel()
	base := zipBytes(t, []file{
		{name: "GAME/.images/cover.png", contents: "png"},
		{name: "GAME/INSTALL.BAT", contents: "install"},
		{name: "GAME/game.exe", contents: "executable"},
	})
	first, err := New(bytes.NewReader(base), int64(len(base)), "GAME/game.exe")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(bytes.NewReader(base), int64(len(base)), "GAME/game.exe")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := io.ReadAll(second)
	if err != nil || !bytes.Equal(contents, repeated) {
		t.Fatalf("overlay drift: error=%v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 4 || reader.File[0].Name != "AUTOBOOT.DBP" {
		names := make([]string, 0, len(reader.File))
		for _, entry := range reader.File {
			names = append(names, entry.Name)
		}
		t.Fatalf("overlay entries = %#v", names)
	}
	if got := readFile(t, reader.File[0]); got != `C:\GAME\GAME.EXE` {
		t.Fatalf("AUTOBOOT.DBP = %q", got)
	}
	if got := readFile(t, reader.File[3]); got != "executable" {
		t.Fatalf("preserved executable = %q", got)
	}
	if first.Size() != int64(len(contents)) {
		t.Fatalf("overlay size = %d, want %d", first.Size(), len(contents))
	}
	if _, err := first.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	fragment := make([]byte, 17)
	if _, err := io.ReadFull(first, fragment); err != nil || !bytes.Equal(fragment, contents[5:22]) {
		t.Fatalf("seeked fragment = %x, error=%v", fragment, err)
	}
	readAtFragment := make([]byte, 19)
	if _, err := first.ReadAt(readAtFragment, int64(len(contents)-len(readAtFragment))); err != nil ||
		!bytes.Equal(readAtFragment, contents[len(contents)-len(readAtFragment):]) {
		t.Fatalf("ReadAt fragment = %x, error=%v", readAtFragment, err)
	}
}

func TestOverlayCallsBatchEntriesAndRejectsInvalidZIP(t *testing.T) {
	t.Parallel()
	base := zipBytes(t, []file{{name: "PLAY.BAT", contents: "play"}})
	overlay, err := New(bytes.NewReader(base), int64(len(base)), "PLAY.BAT")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(overlay)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, reader.File[0]); got != `C:\PLAY.BAT` {
		t.Fatalf("autoboot launcher = %q", got)
	}
	if _, err := New(bytes.NewReader([]byte("not a zip")), 9, "PLAY.BAT"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid ZIP error = %v", err)
	}
}

func TestOverlayReplacesAnExistingReservedLauncher(t *testing.T) {
	t.Parallel()
	base := zipBytes(t, []file{
		{name: "dosbox.bat", contents: "untrusted launcher"},
		{name: "autoboot.dbp", contents: "untrusted autoboot"},
		{name: "GAME.EXE", contents: "executable"},
	})
	overlay, err := New(bytes.NewReader(base), int64(len(base)), "GAME.EXE")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(overlay)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 2 || reader.File[0].Name != "AUTOBOOT.DBP" || reader.File[1].Name != "GAME.EXE" {
		t.Fatalf("overlay entries = %#v", reader.File)
	}
	if got := readFile(t, reader.File[0]); strings.Contains(got, "untrusted") || got != `C:\GAME.EXE` {
		t.Fatalf("replacement launcher = %q", got)
	}
}

func TestOverlayUsesDOSBoxPure83AliasesIncludingCollisions(t *testing.T) {
	t.Parallel()
	base := zipBytes(t, []file{
		{name: "Long Folder/ABCD1111WXYZ.EXE", contents: "first"},
		{name: "Long Folder/ABCD2222WXYZ.EXE", contents: "selected"},
	})
	overlay, err := New(bytes.NewReader(base), int64(len(base)), "Long Folder/ABCD2222WXYZ.EXE")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(overlay)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, reader.File[0]); got != `C:\LONGLDER\ABCDXXYZ.EXE` {
		t.Fatalf("8.3 collision launcher = %q", got)
	}
}

func TestOverlayMatchesLegacyGB18030EntryNameUsedByImportScan(t *testing.T) {
	t.Parallel()
	encoded, err := simplifiedchinese.GB18030.NewEncoder().String("金庸群侠传/PLAY.BAT")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	untrusted, err := writer.Create("RETROM.BAT")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(untrusted, "untrusted"); err != nil {
		t.Fatal(err)
	}
	destination, err := writer.CreateHeader(&zip.FileHeader{
		Name: encoded, NonUTF8: true, Method: zip.Store,
	})
	if err == nil {
		_, err = io.WriteString(destination, "@ECHO OFF\r\nZ\r\n")
	}
	if err == nil {
		var after io.Writer
		after, err = writer.Create("AFTER.TXT")
		if err == nil {
			_, err = io.WriteString(after, "after")
		}
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	overlay, err := New(bytes.NewReader(output.Bytes()), int64(output.Len()), "金庸群侠传/PLAY.BAT")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(overlay)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 4 || reader.File[0].Name != "AUTOBOOT.DBP" || reader.File[1].Name != "RETROM.BAT" ||
		reader.File[2].Name != "RT93B32B/PLAY.BAT" {
		t.Fatalf("legacy-name overlay entries = %#v", reader.File)
	}
	if got, want := readFile(t, reader.File[0]), `C:\RT93B32B\PLAY.BAT`; got != want {
		t.Fatalf("legacy-name AUTOBOOT.DBP = %x, want %x", got, want)
	}
	if got, want := readFile(t, reader.File[1]), "untrusted"; got != want {
		t.Fatalf("source RETROM.BAT = %x, want %x", got, want)
	}
	if got, want := readFile(t, reader.File[2]), "@ECHO OFF\r\nZ\r\n"; got != want {
		t.Fatalf("rewritten selected entry contents = %x, want %x", got, want)
	}
	if got, want := readFile(t, reader.File[3]), "after"; got != want {
		t.Fatalf("entry after rewritten local name = %q, want %q", got, want)
	}
}

func TestOverlayAvoidsLegacyDirectoryMappingCollision(t *testing.T) {
	t.Parallel()
	directory, err := simplifiedchinese.GB18030.NewEncoder().String("金庸群侠传")
	if err != nil {
		t.Fatal(err)
	}
	firstMapping := legacyReplacement(directory, 0, false)
	secondMapping := legacyReplacement(directory, 1, false)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	reserved, err := writer.Create(firstMapping + "/KEEP.TXT")
	if err == nil {
		_, err = io.WriteString(reserved, "keep")
	}
	var selected io.Writer
	if err == nil {
		selected, err = writer.CreateHeader(&zip.FileHeader{
			Name: directory + "/PLAY.BAT", NonUTF8: true, Method: zip.Store,
		})
	}
	if err == nil {
		_, err = io.WriteString(selected, "play")
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	overlay, err := New(bytes.NewReader(output.Bytes()), int64(output.Len()), "金庸群侠传/PLAY.BAT")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(overlay)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, reader.File[0]), `C:\`+secondMapping+`\PLAY.BAT`; got != want {
		t.Fatalf("collision-safe AUTOBOOT.DBP = %q, want %q", got, want)
	}
	if reader.File[1].Name != firstMapping+"/KEEP.TXT" || reader.File[2].Name != secondMapping+"/PLAY.BAT" {
		t.Fatalf("collision-safe entries = %#v", reader.File)
	}
}

func TestMenuOverlayUsesPureMenuAndRemovesAutoboot(t *testing.T) {
	t.Parallel()
	base := zipBytes(t, []file{
		{name: "AUTOBOOT.DBP", contents: `C:\GAME.EXE`},
		{name: "GAME.EXE", contents: "executable"},
	})
	overlay, err := NewMenu(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(overlay)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 2 || reader.File[0].Name != "DOSBOX.BAT" || reader.File[1].Name != "GAME.EXE" {
		t.Fatalf("menu overlay entries = %#v", reader.File)
	}
	if got := readFile(t, reader.File[0]); got != "@ECHO OFF\r\nZ:\\PUREMENU\r\n" {
		t.Fatalf("menu launcher = %q", got)
	}
}

type file struct{ name, contents string }

func zipBytes(t *testing.T, files []file) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, source := range files {
		destination, err := writer.Create(source.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(destination, source.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func readFile(t *testing.T, source *zip.File) string {
	t.Helper()
	reader, err := source.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}()
	contents, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

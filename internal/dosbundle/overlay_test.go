package dosbundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
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

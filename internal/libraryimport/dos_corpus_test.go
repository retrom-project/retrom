package libraryimport

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"retrom/internal/dosbundle"
	"retrom/internal/importing"
)

// TestLocalDOSCorpusCompatibility is an opt-in structural check for a user's
// legal local DOS library. Regular CI skips it because ROMs are never tracked.
func TestLocalDOSCorpusCompatibility(t *testing.T) {
	root := os.Getenv("RETROM_DOS_CORPUS")
	if root == "" {
		t.Skip("RETROM_DOS_CORPUS is not configured")
	}
	archives := make([]string, 0)
	//nolint:gosec // The operator explicitly opts this test into traversing their local legal corpus root.
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			archives = append(archives, path)
		}
		return nil
	})
	if err != nil || len(archives) == 0 {
		t.Fatalf("DOS corpus contains no ZIP archives: count=%d error=%v", len(archives), err)
	}
	sort.Strings(archives)
	var direct, menuOnly int
	for _, archivePath := range archives {
		entries, scanErr := importing.ScanZIP(context.Background(), archivePath, importing.DOSArchiveLimits())
		if scanErr != nil {
			t.Errorf("%s: scan: %v", filepath.Base(archivePath), scanErr)
			continue
		}
		programs := make([]preparedDOSEntry, 0)
		for _, entry := range entries {
			if kind, ok := dosProgram(entry.NormalizedPath); ok {
				programs = append(programs, preparedDOSEntry{
					path: entry.NormalizedPath, kind: kind, safe: directDOSPathSafe(entry.NormalizedPath),
				})
			}
		}
		if len(programs) == 0 {
			t.Errorf("%s: no executable candidates", filepath.Base(archivePath))
			continue
		}
		rankDOSEntries(programs)
		file, openErr := os.Open(archivePath) //nolint:gosec // Explicit opt-in test corpus path supplied by its operator.
		if openErr != nil {
			t.Errorf("%s: open: %v", filepath.Base(archivePath), openErr)
			continue
		}
		stat, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			t.Errorf("%s: stat: %v", filepath.Base(archivePath), statErr)
			continue
		}
		selected := ""
		for _, program := range programs {
			if program.safe {
				selected = program.path
				break
			}
		}
		var overlay *dosbundle.Overlay
		if selected == "" {
			menuOnly++
			overlay, err = dosbundle.NewMenu(file, stat.Size())
		} else {
			direct++
			overlay, err = dosbundle.New(file, stat.Size(), selected)
		}
		if err != nil {
			_ = file.Close()
			t.Errorf("%s: overlay %q: %v", filepath.Base(archivePath), selected, err)
			continue
		}
		reader, zipErr := zip.NewReader(overlay, overlay.Size())
		_ = file.Close()
		expectedLauncher := "AUTOBOOT.DBP"
		if selected == "" {
			expectedLauncher = "DOSBOX.BAT"
		}
		if zipErr != nil || len(reader.File) == 0 || !strings.EqualFold(reader.File[0].Name, expectedLauncher) {
			t.Errorf("%s: invalid overlay ZIP: entries=%d error=%v", filepath.Base(archivePath), len(reader.File), zipErr)
		}
	}
	t.Logf("validated %d DOS archives: %d direct, %d program-menu only", len(archives), direct, menuOnly)
}

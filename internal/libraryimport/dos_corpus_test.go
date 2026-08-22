package libraryimport

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"retrom/internal/dosbundle"
	"retrom/internal/importing"
	"retrom/internal/testassert"
)

// TestLocalDOSCorpusCompatibility is an opt-in structural check for a user's
// legal local DOS library. Regular CI skips it because ROMs are never tracked.
func TestLocalDOSCorpusCompatibility(t *testing.T) {
	root := os.Getenv("RETROM_DOS_CORPUS")
	if root == "" {
		t.Skip("RETROM_DOS_CORPUS is not configured")
	}
	archives, err := discoverDOSCorpusArchives(root)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(archives) == 0 }), "DOS corpus contains no ZIP archives: count=%d error=%v", len(archives), err)
	sort.Strings(archives)
	var direct, menuOnly int
	for _, archivePath := range archives {
		menu, validateErr := validateDOSCorpusArchive(archivePath)
		if validateErr != nil {
			t.Errorf("%s: %v", filepath.Base(archivePath), validateErr)
			continue
		}
		if menu {
			menuOnly++
		} else {
			direct++
		}
	}
	t.Logf("validated %d DOS archives: %d direct, %d program-menu only", len(archives), direct, menuOnly)
}

func discoverDOSCorpusArchives(root string) ([]string, error) {
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
	return archives, err
}

func validateDOSCorpusArchive(archivePath string) (bool, error) {
	entries, err := importing.ScanZIP(context.Background(), archivePath, importing.DOSArchiveLimits())
	if err != nil {
		return false, fmt.Errorf("scan: %w", err)
	}
	programs := corpusDOSPrograms(entries)
	if len(programs) == 0 {
		return false, errors.New("no executable candidates")
	}
	rankDOSEntries(programs)
	file, err := os.Open(archivePath) //nolint:gosec // Explicit opt-in test corpus path supplied by its operator.
	if err != nil {
		return false, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("stat: %w", err)
	}
	selected := firstSafeDOSProgram(programs)
	overlay, err := corpusDOSOverlay(file, stat.Size(), selected)
	if err != nil {
		return false, fmt.Errorf("overlay %q: %w", selected, err)
	}
	reader, err := zip.NewReader(overlay, overlay.Size())
	if err != nil || len(reader.File) == 0 {
		return false, fmt.Errorf("invalid overlay ZIP: %w", err)
	}
	expectedLauncher := "AUTOBOOT.DBP"
	if selected == "" {
		expectedLauncher = "DOSBOX.BAT"
	}
	if !strings.EqualFold(reader.File[0].Name, expectedLauncher) {
		return false, fmt.Errorf("invalid overlay launcher %q", reader.File[0].Name)
	}
	return selected == "", nil
}

func corpusDOSPrograms(entries []importing.ArchiveEntry) []preparedDOSEntry {
	programs := make([]preparedDOSEntry, 0)
	for _, entry := range entries {
		if kind, ok := dosProgram(entry.NormalizedPath); ok {
			programs = append(programs, preparedDOSEntry{
				path: entry.NormalizedPath, kind: kind, safe: directDOSPathSafe(entry.NormalizedPath),
			})
		}
	}
	return programs
}

func firstSafeDOSProgram(programs []preparedDOSEntry) string {
	for _, program := range programs {
		if program.safe {
			return program.path
		}
	}
	return ""
}

func corpusDOSOverlay(file *os.File, size int64, selected string) (*dosbundle.Overlay, error) {
	if selected == "" {
		return dosbundle.NewMenu(file, size)
	}
	return dosbundle.New(file, size, selected)
}

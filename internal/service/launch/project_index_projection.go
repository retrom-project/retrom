package launch

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

func projectIndexProjection(snapshot ProjectIndexSnapshot) ([]runtimeProjectIndexFile, string, string, error) {
	root, format, err := projectIndexRoot(snapshot)
	if err != nil {
		return nil, "", "", err
	}
	preview := snapshot.Source.Purpose == "REVIEW_PREVIEW"
	if preview && snapshot.Source.ContentKind != format {
		return nil, "", "", ErrCredential
	}
	ordered := slices.Clone(snapshot.Files)
	slices.SortFunc(ordered, func(left, right ProjectIndexRecord) int {
		if preview && format != "SCUMMVM_PROJECT" {
			return comparePreviewIndexFiles(left, right)
		}
		return strings.Compare(left.Content.LogicalName, right.Content.LogicalName)
	})
	files := make([]runtimeProjectIndexFile, 0, len(ordered))
	for _, file := range ordered {
		if preview && !file.Primary && file.Content.Role == "RUNTIME_FILE" && format != "ONS_PROJECT" {
			continue
		}
		files = append(files, runtimeProjectIndexFile{Path: file.Content.LogicalName, SizeBytes: file.Content.Size})
	}
	if preview && format == "ONS_PROJECT" && !ordered[0].Primary {
		return nil, "", "", ErrCredential
	}
	return files, root, format, nil
}

func projectIndexRoot(snapshot ProjectIndexSnapshot) (string, string, error) {
	if len(snapshot.Files) == 0 {
		return "", "", ErrCredential
	}
	identityFiles := make([]ConfigFile, 0, len(snapshot.Files))
	for _, file := range snapshot.Files {
		identityFiles = append(identityFiles, file.Content)
	}
	identity, err := ProjectIdentity(identityFiles)
	if err != nil {
		return "", "", fmt.Errorf("%w: project identity: %w", ErrCredential, err)
	}
	root, err := RuntimeProjectContentRoot(identity)
	if err != nil {
		return "", "", fmt.Errorf("%w: project root: %w", ErrCredential, err)
	}
	return root, identityFiles[0].Format, nil
}

func comparePreviewIndexFiles(left, right ProjectIndexRecord) int {
	if left.Primary != right.Primary {
		if left.Primary {
			return -1
		}
		return 1
	}
	if order := cmp.Compare(left.Order, right.Order); order != 0 {
		return order
	}
	return strings.Compare(left.Content.LogicalName, right.Content.LogicalName)
}

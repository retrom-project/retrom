package filestore

import "strings"

// CleanupDirectory returns the immutable workspace containing a file. Replacements
// always allocate a fresh workspace, so delayed cleanup cannot remove new data.
func CleanupDirectory(value string) (string, error) {
	file, err := ParseRecord(value)
	if err != nil {
		return "", err
	}
	parts := strings.Split(file.Path, "/")
	count := 0
	switch parts[0] {
	case "files":
		if len(parts) > 5 && (parts[3] == "content" || parts[3] == "media") {
			count = 5
		}
	case "saves":
		count = 3
	case "previews":
		count = previewCleanupDepth(parts)
	case "bios", "scrapes", "responses":
		count = 2
	case "staging":
		count = stagingCleanupDepth(parts)
	}
	if count < 0 {
		return "", nil
	}
	if count == 0 || len(parts) <= count {
		return "", ErrRecordInvalid
	}
	return strings.Join(parts[:count], "/"), nil
}

func stagingCleanupDepth(parts []string) int {
	switch parts[1] {
	case "writes":
		return -1
	case "items":
		if len(parts) > 6 {
			return 6
		}
	case "uploads", "sources":
		return 3
	}
	return 0
}

func previewCleanupDepth(parts []string) int {
	if len(parts) > 4 && parts[2] == "checkpoints" {
		return 4
	}
	if len(parts) > 3 && parts[2] == "restore" {
		return 3
	}
	return 0
}

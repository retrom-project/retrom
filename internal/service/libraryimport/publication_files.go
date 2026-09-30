package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"retrom/internal/filestore"
)

type PublicationFile struct{ Source, Staged string }

func stagePublicationRuntimeFiles(files []PreparedValidationFile, staged []PublicationFile) []PreparedValidationFile {
	bySource := make(map[string]string, len(staged))
	for _, file := range staged {
		bySource[file.Source] = file.Staged
	}
	result := append([]PreparedValidationFile(nil), files...)
	for index := range result {
		if record, found := bySource[result[index].FileRecord]; found {
			result[index].FileRecord = record
		}
	}
	return result
}

func publicationFiles(values []string, itemID, snapshotID string) ([]PublicationFile, error) {
	prefix := filestore.ItemDirectory(itemID) + "/payload/"
	files := make([]PublicationFile, 0, len(values))
	for _, value := range values {
		record, err := filestore.ParseRecord(value)
		if err != nil {
			return nil, fmt.Errorf("publication files: %w", err)
		}
		if strings.HasPrefix(record.Path, prefix) {
			continue
		}
		digest := sha256.Sum256([]byte(record.Path))
		record.Path = prefix + "content/" + snapshotID + "/" + hex.EncodeToString(digest[:])
		staged, err := record.Encode()
		if err != nil {
			return nil, fmt.Errorf("publication files: %w", err)
		}
		files = append(files, PublicationFile{Source: value, Staged: staged})
	}
	return files, nil
}

func (service *ReviewApprovals) copyPublicationFiles(ctx context.Context, files []PublicationFile) error {
	for _, file := range files {
		record, err := filestore.ParseRecord(file.Staged)
		if err != nil {
			return fmt.Errorf("copy publication files: %w", err)
		}
		if _, err := service.files.CopyTo(ctx, file.Source, path.Dir(record.Path), path.Base(record.Path)); err != nil {
			return fmt.Errorf("copy publication files: %w", err)
		}
	}
	return nil
}

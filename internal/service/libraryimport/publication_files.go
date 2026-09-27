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

func publicationFiles(values []string, itemID, validationID string) ([]PublicationFile, error) {
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
		record.Path = prefix + "content/" + validationID + "/" + hex.EncodeToString(digest[:])
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

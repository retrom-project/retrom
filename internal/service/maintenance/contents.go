package maintenance

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	model "retrom/internal/model/maintenance"
)

func referencedFiles(snapshot model.Snapshot) ([]FileEntry, error) {
	files := make([]FileEntry, 0, len(snapshot.Blobs)+len(snapshot.Parts))
	for _, blob := range snapshot.Blobs {
		if !validDigest(blob.SHA256) || blob.SizeBytes < 0 {
			return nil, model.ErrInvalidBundle
		}
		files = append(files, FileEntry{
			Path: "blobs/sha256/" + blob.SHA256[:2] + "/" + blob.SHA256[2:4] + "/" + blob.SHA256,
			Kind: "CAS_BLOB", SizeBytes: blob.SizeBytes, SHA256: blob.SHA256, Mode: "0600",
		})
	}
	for _, part := range snapshot.Parts {
		if !safeStorageKey(part.StorageKey) || !validDigest(part.SHA256) || part.SizeBytes < 0 {
			return nil, model.ErrInvalidBundle
		}
		files = append(
			files,
			FileEntry{
				Path:      "tmp/uploads/" + part.StorageKey,
				Kind:      "UPLOAD_PART",
				SizeBytes: part.SizeBytes,
				SHA256:    part.SHA256,
				Mode:      "0600",
			},
		)
	}
	return files, nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	bytes, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(bytes) == value
}

func copyBackupContents(ctx context.Context, snapshot model.Snapshot, root, staging string, manifest *Manifest) error {
	files, err := referencedFiles(snapshot)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("copy backup contents: %w", err)
		}
		entry, err := copyVerified(
			filepath.Join(
				root,
				filepath.FromSlash(
					file.Path,
				),
			),
			filepath.Join(
				staging,
				filepath.FromSlash(
					file.Path,
				),
			),

			file.Path,
			file.Kind,
			file.SHA256,
		)
		if err != nil {
			return err
		}
		if entry.SizeBytes != file.SizeBytes {
			return model.ErrInvalidBundle
		}
		manifest.Files = append(manifest.Files, entry)
		if entry.Kind == "CAS_BLOB" {
			manifest.Counts.BlobCount++
		} else {
			manifest.Counts.UploadPartCount++
		}
	}
	return nil
}

func validateRestoredContents(ctx context.Context, snapshot model.Snapshot, root string) error {
	files, err := referencedFiles(snapshot)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("validate restored contents: %w", err)
		}
		digest, size, err := digestRegular(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			return err
		}
		if digest != file.SHA256 || size != file.SizeBytes {
			return model.ErrInvalidBundle
		}
	}
	return nil
}

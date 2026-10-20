package providersync

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ensureChunk makes cache/<chunkId>/ hold the verified files of one chunk. A
// published chunk directory is always complete, so its presence means verified.
func (s *syncer) ensureChunk(ctx context.Context, chunk Chunk) (bool, error) {
	destination := filepath.Join(s.cache, chunk.ChunkID)
	if exists(destination) {
		return false, nil
	}
	staging := filepath.Join(s.cache, ".staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return false, fmt.Errorf("create chunk staging: %w", err)
	}
	archive := filepath.Join(staging, chunk.ChunkID+".tar.gz")
	extracted := filepath.Join(staging, "chunk-"+chunk.ChunkID)
	defer func() {
		_ = os.Remove(archive)
		_ = os.RemoveAll(extracted)
	}()
	if err := os.RemoveAll(extracted); err != nil {
		return false, fmt.Errorf("reset chunk staging: %w", err)
	}
	seeded, err := s.seedChunk(chunk, extracted)
	if err != nil {
		return false, err
	}
	if !seeded {
		if err = s.download(ctx, chunk, archive); err != nil {
			return false, err
		}
		if err = extractChunk(archive, extracted, chunk); err != nil {
			return false, err
		}
	}
	if err = os.Rename(extracted, destination); err != nil {
		return false, fmt.Errorf("publish chunk %s: %w", chunk.ChunkID, err)
	}
	if seeded {
		s.result.SeededChunks++
		return false, syncDirectory(s.cache)
	}
	return true, syncDirectory(s.cache)
}

// seedChunk links a chunk from a local seed when every file matches the manifest exactly;
// anything else is ignored and downloaded instead.
func (s *syncer) seedChunk(chunk Chunk, target string) (bool, error) {
	for _, seed := range s.options.Seeds {
		// A seed is a chunk cache (<seed>/<id>) or a Provider root (<seed>/chunks/<id>).
		source := seedSource(seed, chunk)
		if source == "" {
			continue
		}
		for _, file := range chunk.Files {
			path := filepath.Join(target, filepath.FromSlash(file.Path))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return false, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
			}
			if err := linkOrCopy(filepath.Join(source, filepath.FromSlash(file.Path)), path); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	return false, nil
}

// seedSource finds a complete copy of the chunk in a chunk cache (<seed>/<id>), a Provider
// root (<seed>/chunks/<id>) or any installed bundle of a root (<seed>/installed/*/*/<path>).
func seedSource(seed string, chunk Chunk) string {
	candidates := []string{filepath.Join(seed, chunk.ChunkID), filepath.Join(seed, "chunks", chunk.ChunkID)}
	bundles, _ := filepath.Glob(filepath.Join(seed, "installed", "*", "*"))
	for _, candidate := range append(candidates, bundles...) {
		if seedComplete(candidate, chunk) {
			return candidate
		}
	}
	return ""
}

// seedComplete accepts a seed only when every file has the manifest's exact size and
// SHA-256: equal paths and sizes in another version's bundle can still hold other bytes.
func seedComplete(source string, chunk Chunk) bool {
	for _, file := range chunk.Files {
		path := filepath.Join(source, filepath.FromSlash(file.Path))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != file.SizeBytes || hashFile(path) != file.SHA256 {
			return false
		}
	}
	return true
}

func (s *syncer) download(ctx context.Context, chunk Chunk, path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create chunk archive: %w", err)
	}
	asset := Asset{Asset: chunk.Archive, SHA256: chunk.ArchiveSHA256, SizeBytes: chunk.ArchiveSizeBytes}
	source, err := s.transport()
	if err != nil {
		_ = file.Close()
		return err
	}
	fetchErr := fetchVerified(ctx, source, asset, file)
	closeErr := file.Close()
	if err = errors.Join(fetchErr, closeErr); err != nil {
		return fmt.Errorf("download chunk %s: %w", chunk.ChunkID, err)
	}
	return nil
}

// extractChunk accepts only the regular files the manifest lists, in order, with exact bytes.
func extractChunk(archive, target string, chunk Chunk) error {
	file, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open chunk archive: %w", err)
	}
	defer func() { _ = file.Close() }()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("chunk %s: %w", chunk.ChunkID, ErrChunkInvalid)
	}
	reader := tar.NewReader(compressed)
	for _, expected := range chunk.Files {
		header, nextErr := reader.Next()
		if nextErr != nil || header.Typeflag != tar.TypeReg || header.Name != expected.Path ||
			header.Size != expected.SizeBytes {
			return fmt.Errorf("chunk %s entry %s: %w", chunk.ChunkID, expected.Path, ErrChunkInvalid)
		}
		if err = writeVerified(filepath.Join(target, filepath.FromSlash(expected.Path)), reader, expected); err != nil {
			return fmt.Errorf("chunk %s: %w", chunk.ChunkID, err)
		}
	}
	if _, err = reader.Next(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("chunk %s has extra entries: %w", chunk.ChunkID, ErrChunkInvalid)
	}
	return nil
}

func writeVerified(path string, reader io.Reader, expected ChunkFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(reader, expected.SizeBytes+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	if err = errors.Join(copyErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write %s: %w", expected.Path, err)
	}
	if written != expected.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		return fmt.Errorf("file %s: %w", expected.Path, ErrChunkInvalid)
	}
	return nil
}

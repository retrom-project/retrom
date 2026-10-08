package scans

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"retrom/internal/model"
	"retrom/internal/storage"

	"github.com/bodgit/sevenzip"
	"github.com/google/uuid"
)

type entry struct {
	name string
	size int64
	open func() (io.ReadCloser, error)
}

func (s *Service) content(ctx context.Context,
	root *os.Root,
	id string,
	files []string,
	directory model.Directory) ([]model.GameFile,
	error,
) {
	if len(files) == 0 || len(files) > 64 {
		return nil, model.ErrInvalid
	}
	result := make([]model.GameFile, 0)
	origins := make(map[string]string)
	for _, name := range files {
		part, sources, err := s.contentFile(ctx, root, id, name, directory)
		if err != nil {
			return nil, err
		}
		result = append(result, part...)
		for key, source := range sources {
			origins[key] = source
		}
	}
	if len(result) > 10000 {
		return nil, model.ErrInvalid
	}
	return s.discoverContent(ctx, root, id, result, origins)
}

func (s *Service) contentFile(ctx context.Context, root *os.Root, id, name string,
	directory model.Directory,
) ([]model.GameFile, map[string]string, error) {
	name = strings.TrimPrefix(name, "./")
	if !storage.SafeRelative(name) {
		return nil, nil, model.ErrInvalid
	}
	info, err := root.Stat(name)
	if err != nil {
		return nil, nil, fmt.Errorf("source file: %w", err)
	}
	if info.IsDir() {
		files, walkErr := s.walk(ctx, root, id, name, name)
		origins := make(map[string]string, len(files))
		for _, file := range files {
			origins[file.LogicalKey] = path.Join(name, file.LogicalKey)
		}
		return files, origins, walkErr
	}
	if !info.Mode().IsRegular() {
		return nil, nil, model.ErrInvalid
	}
	ext := strings.ToLower(path.Ext(name))
	if (ext == ".zip" || ext == ".7z") && !s.keepArchive(directory) {
		files, archiveErr := s.archive(ctx, root, id, name, ext)
		return files, nil, archiveErr
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, nil, fmt.Errorf("open source game: %w", err)
	}
	defer closeFile(file)
	stored, err := s.copy(ctx, id, path.Base(name), file)
	if err != nil {
		return nil, nil, err
	}
	return []model.GameFile{stored}, map[string]string{stored.LogicalKey: name}, nil
}

func (s *Service) keepArchive(directory model.Directory) bool {
	for _, binding := range s.Runtime.Bindings {
		if binding.CoreID != directory.DefaultCoreID {
			continue
		}
		for _, kind := range binding.ContentKinds {
			if kind == "ARCADE" || kind == "DOS_BUNDLE" {
				return true
			}
		}
	}
	return false
}

func (s *Service) walk(ctx context.Context, root *os.Root, id, base, current string) ([]model.GameFile, error) {
	if strings.Count(current, "/") > 24 {
		return nil, model.ErrInvalid
	}
	entries, err := readDir(root, current)
	if err != nil {
		return nil, err
	}
	result := make([]model.GameFile, 0)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, model.ErrInvalid
		}
		name := path.Join(current, entry.Name())
		if entry.IsDir() {
			children, walkErr := s.walk(ctx, root, id, base, name)
			if walkErr != nil {
				return nil, walkErr
			}
			result = append(result, children...)
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, model.ErrInvalid
		}
		file, readErr := root.Open(name)
		if readErr != nil {
			return nil, fmt.Errorf("open project file: %w", readErr)
		}
		logical := strings.TrimPrefix(name, base+"/")
		stored, copyErr := s.copy(ctx, id, logical, file)
		closeErr := file.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close project file: %w", closeErr)
		}
		result = append(result, stored)
		if len(result) > 10000 {
			return nil, model.ErrInvalid
		}
	}
	return result, nil
}

func (s *Service) copy(ctx context.Context, id, name string, reader io.Reader) (model.GameFile, error) {
	if !storage.SafeRelative(name) {
		return model.GameFile{}, model.ErrInvalid
	}
	file, err := s.Storage.Write(ctx, "games", id, reader, storage.MaximumFileSize)
	if err != nil {
		return model.GameFile{}, wrap(err)
	}
	return model.GameFile{
			ID:         uuid.NewString(),
			LogicalKey: name,
			Role:       "content",
			StorageKey: file.Key,
			SHA256:     file.SHA256,
			SizeBytes:  file.Size,
		},
		nil
}

func (s *Service) archive(ctx context.Context, root *os.Root, id, name, ext string) ([]model.GameFile, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer closeFile(file)
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat archive: %w", err)
	}
	var entries []entry
	if ext == ".zip" {
		entries, err = zipEntries(file, info.Size())
	} else {
		entries, err = sevenEntries(file, info.Size())
	}
	if err != nil {
		return nil, err
	}

	return s.extract(ctx, id, entries)
}

func (s *Service) extract(ctx context.Context, id string, entries []entry) ([]model.GameFile, error) {
	if len(entries) == 0 || len(entries) > 10000 {
		return nil, model.ErrInvalid
	}
	seen := make(map[string]bool, len(entries))
	var total int64
	result := make([]model.GameFile, 0, len(entries))
	for _, entry := range entries {
		if !storage.SafeRelative(entry.name) || seen[entry.name] || entry.size < 0 {
			return nil, model.ErrInvalid
		}
		seen[entry.name] = true
		total += entry.size
		if total > storage.MaximumFileSize {
			return nil, model.ErrInvalid
		}
		reader, err := entry.open()
		if err != nil {
			return nil, fmt.Errorf("read archive member: %w", err)
		}
		file, copyErr := s.copy(ctx, id, entry.name, reader)
		closeErr := reader.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close archive member: %w", closeErr)
		}
		result = append(result, file)
	}
	return result, nil
}

func zipEntries(file *os.File, size int64) ([]entry, error) {
	archive, err := zip.NewReader(file, size)
	if err != nil {
		return nil, fmt.Errorf("read ZIP: %w", err)
	}
	entries := make([]entry, 0, len(archive.File))
	for _, member := range archive.File {
		if !member.FileInfo().IsDir() {
			entries = append(entries, entry{
				name: archiveName(member.Name), size: int64(member.UncompressedSize64),
				open: member.Open,
			})
		}
	}
	return entries, nil
}

func sevenEntries(file *os.File, size int64) ([]entry, error) {
	archive, err := sevenzip.NewReader(file, size)
	if err != nil {
		return nil, fmt.Errorf("read 7z: %w", err)
	}
	entries := make([]entry, 0, len(archive.File))
	for _, member := range archive.File {
		if !member.FileInfo().IsDir() {
			entries = append(entries, entry{name: member.Name, size: int64(member.UncompressedSize), open: member.Open})
		}
	}
	return entries, nil
}

func archiveName(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes([]byte(value))
	if err != nil {
		return ""
	}
	return string(decoded)
}

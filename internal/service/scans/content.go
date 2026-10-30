package scans

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"retrom/internal/model"
	"retrom/internal/storage"

	"github.com/bodgit/sevenzip"
	"github.com/google/uuid"
)

const maximumGameFiles = 50_000

// Inputs are operation-owned copies before extraction or runtime normalization.
// They describe only this candidate, including any discovered companion inputs.
type sourceContent struct {
	Files  []model.ContentFile
	Inputs []model.ContentFile
	Bundle bool
}

type entry struct {
	name string
	size int64
	open func() (io.ReadCloser, error)
}

// A project consists of independently bounded files, rather than one upload.
const maximumContentSize int64 = 64 * 1024 * 1024 * 1024

func (s *Service) content(ctx context.Context,
	root *os.Root,
	id string,
	files []string,
	directory model.Directory, inventory *sourceInventory) (sourceContent,
	error,
) {
	ctx = context.WithValue(ctx, contentBudgetKey{}, &contentBudget{})
	if len(files) == 0 || len(files) > 64 {
		return sourceContent{}, model.ErrInvalid
	}
	result := sourceContent{}
	origins := make(map[string]string)
	for _, name := range files {
		part, sources, err := s.contentFile(ctx, root, id, name, directory)
		if err != nil {
			return sourceContent{}, err
		}
		result.Files = append(result.Files, part.Files...)
		result.Inputs = append(result.Inputs, part.Inputs...)
		result.Bundle = result.Bundle || part.Bundle
		if err = result.limitOriginals(); err != nil {
			return sourceContent{}, err
		}
		for key, source := range sources {
			origins[key] = source
		}
	}
	if len(result.Files) > maximumGameFiles {
		return sourceContent{}, model.ErrInvalid
	}
	err := s.discoverContent(ctx, root, id, &result, origins, directory, inventory)
	if err != nil {
		return sourceContent{}, err
	}
	if err = storage.ValidateContent(result.Files); err != nil {
		return sourceContent{}, wrap(err)
	}
	if err = result.limitOriginals(); err != nil {
		return sourceContent{}, err
	}
	result.Bundle = result.Bundle || len(result.Inputs) != 1
	return result, nil
}

func (c *sourceContent) limitOriginals() error {
	if len(c.Inputs) > maximumGameFiles {
		return model.ErrInvalid
	}
	var total int64
	seen := make(map[string]model.ContentFile, len(c.Inputs))
	unique := make([]model.ContentFile, 0, len(c.Inputs))
	for _, input := range c.Inputs {
		if previous, exists := seen[input.LogicalKey]; exists {
			if previous.SHA256 != input.SHA256 || previous.SizeBytes != input.SizeBytes {
				return model.ErrInvalid
			}
			continue
		}
		if input.SizeBytes < 0 || input.SizeBytes > storage.MaximumPackageSize-total {
			return model.ErrInvalid
		}
		total += input.SizeBytes
		seen[input.LogicalKey] = input
		unique = append(unique, input)
	}
	c.Inputs = unique
	return nil
}

func (s *Service) contentFile(ctx context.Context, root *os.Root, id, name string,
	directory model.Directory,
) (sourceContent, map[string]string, error) {
	name = strings.TrimPrefix(name, "./")
	if name != "." && !storage.SafeRelative(name) {
		return sourceContent{}, nil, model.ErrInvalid
	}
	info, err := root.Stat(name)
	if err != nil {
		return sourceContent{}, nil, fmt.Errorf("source file: %w", err)
	}
	if info.IsDir() {
		files, walkErr := s.walk(ctx, root, id, name, name)
		origins := make(map[string]string, len(files))
		inputs := make([]model.ContentFile, 0, len(files))
		for _, file := range files {
			source := path.Join(name, file.LogicalKey)
			origins[file.LogicalKey] = source
			file.LogicalKey = source
			if directory.PlatformID == "kirikiri" {
				inputs = append(inputs, file)
			}
		}
		return sourceContent{Files: files, Inputs: inputs, Bundle: true}, origins, walkErr
	}
	if !info.Mode().IsRegular() {
		return sourceContent{}, nil, model.ErrInvalid
	}
	ext := strings.ToLower(path.Ext(name))
	if (ext == ".zip" || ext == ".7z") && !s.keepArchive(directory, ext) {
		if directory.PlatformID == "kirikiri" {
			files, input, archiveErr := s.archiveSource(ctx, root, id, name, ext)
			return sourceContent{Files: files, Inputs: []model.ContentFile{input}}, nil, archiveErr
		}
		files, archiveErr := s.archive(ctx, root, id, name, ext)
		return sourceContent{Files: files}, nil, archiveErr
	}
	file, err := root.Open(name)
	if err != nil {
		return sourceContent{}, nil, fmt.Errorf("open source game: %w", err)
	}
	defer closeFile(file)
	stored, err := s.copy(ctx, id, path.Base(name), file)
	if err != nil {
		return sourceContent{}, nil, err
	}
	result := sourceContent{Files: []model.ContentFile{stored}}
	if directory.PlatformID == "kirikiri" {
		input := stored
		input.LogicalKey = name
		result.Inputs = []model.ContentFile{input}
	}
	return result, map[string]string{stored.LogicalKey: name}, nil
}

func (s *Service) keepArchive(directory model.Directory, extension string) bool {
	for _, binding := range s.Runtime.Bindings {
		if binding.CoreID != directory.DefaultCoreID || !slices.Contains(binding.PlatformIDs, directory.PlatformID) {
			continue
		}
		if slices.Contains(binding.ArchiveExtensions, strings.TrimPrefix(extension, ".")) {
			return true
		}
		for _, kind := range binding.ContentKinds {
			if kind == "ARCADE" || kind == "DOS_BUNDLE" {
				return true
			}
		}
	}
	return false
}

func (s *Service) walk(ctx context.Context, root *os.Root, id, base, current string) ([]model.ContentFile, error) {
	if strings.Count(current, "/") > 24 {
		return nil, model.ErrInvalid
	}
	entries, err := readDir(root, current)
	if err != nil {
		return nil, err
	}
	result := make([]model.ContentFile, 0)
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
		if len(result) > maximumGameFiles {
			return nil, model.ErrInvalid
		}
	}
	return result, nil
}

func (s *Service) copy(ctx context.Context, id, name string, reader io.Reader) (model.ContentFile, error) {
	if !storage.SafeRelative(name) {
		return model.ContentFile{}, model.ErrInvalid
	}
	maximum, err := contentReadLimit(ctx)
	if err != nil {
		return model.ContentFile{}, err
	}
	file, err := s.Storage.StageLimit(ctx, id, reader, maximum)
	if err != nil {
		return model.ContentFile{}, wrap(err)
	}
	recordContent(ctx, file.Size)
	return model.ContentFile{
			ID:         uuid.NewString(),
			LogicalKey: name,
			Role:       "content",
			StorageKey: file.Key,
			SHA256:     file.SHA256,
			SizeBytes:  file.Size,
		},
		nil
}

func (s *Service) archiveSource(ctx context.Context, root *os.Root, id, name, ext string,
) ([]model.ContentFile, model.ContentFile, error) {
	source, err := root.Open(name)
	if err != nil {
		return nil, model.ContentFile{}, fmt.Errorf("open archive: %w", err)
	}
	defer closeFile(source)
	info, err := source.Stat()
	if err != nil {
		return nil, model.ContentFile{}, fmt.Errorf("stat archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, model.ContentFile{}, model.ErrInvalid
	}
	staged, err := s.Storage.StageOrigin(ctx, id, source, info.Size())
	if err != nil {
		return nil, model.ContentFile{}, wrap(err)
	}
	input := model.ContentFile{LogicalKey: name, StorageKey: staged.Key, SizeBytes: staged.Size, SHA256: staged.SHA256}
	file, err := s.Storage.Read(staged.Key)
	if err != nil {
		return nil, model.ContentFile{}, wrap(err)
	}
	defer closeFile(file)
	files, err := s.archiveFile(ctx, id, file, staged.Size, ext)
	return files, input, err
}

// Ordinary imports continue reading the selected outer archive directly. Only
// KRKR needs an immutable outer source while its internal files may be changed.
func (s *Service) archive(ctx context.Context, root *os.Root, id, name, ext string) ([]model.ContentFile, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer closeFile(file)
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat archive: %w", err)
	}
	return s.archiveFile(ctx, id, file, info.Size(), ext)
}

func (s *Service) archiveFile(ctx context.Context, id string, file *os.File, size int64,
	ext string,
) ([]model.ContentFile, error) {
	var entries []entry
	var err error
	if ext == ".zip" {
		entries, err = zipEntries(file, size)
	} else {
		entries, err = sevenEntries(file, size)
	}
	if err != nil {
		return nil, err
	}
	return s.extract(ctx, id, entries)
}

func (s *Service) extract(ctx context.Context, id string, entries []entry) ([]model.ContentFile, error) {
	if len(entries) == 0 || len(entries) > maximumGameFiles {
		return nil, model.ErrInvalid
	}
	seen := make(map[string]bool, len(entries))
	var total int64
	result := make([]model.ContentFile, 0, len(entries))
	for _, entry := range entries {
		if !storage.SafeRelative(entry.name) || seen[entry.name] || entry.size < 0 || entry.size > storage.MaximumFileSize {
			return nil, model.ErrInvalid
		}
		seen[entry.name] = true
		total += entry.size
		if total > maximumContentSize {
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
		if file.SizeBytes != entry.size {
			return nil, model.ErrInvalid
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
		if member.Flags&1 != 0 || member.Mode()&os.ModeSymlink != 0 {
			return nil, model.ErrInvalid
		}
		if !member.FileInfo().IsDir() {
			name, nameErr := zipMemberName(member)
			if nameErr != nil {
				return nil, nameErr
			}
			entries = append(entries, entry{
				name: name, size: int64(member.UncompressedSize64),
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
		if member.FileInfo().Mode()&os.ModeSymlink != 0 {
			return nil, model.ErrInvalid
		}
		if !member.FileInfo().IsDir() {
			entries = append(entries, entry{name: member.Name, size: int64(member.UncompressedSize), open: member.Open})
		}
	}
	return entries, nil
}

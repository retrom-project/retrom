package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"retrom/internal/model"

	"github.com/google/uuid"
)

// Uploads owns browser upload staging below <data>/uploads/<id>/: files/ is the completed
// source tree a scan reads, parts/<index> holds a file while it is still being received.
type Uploads struct {
	directory string
}

const uploadManifest = "manifest.json"

func OpenUploads(directory string) (Uploads, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return Uploads{}, fmt.Errorf("resolve upload directory: %w", err)
	}
	if err = os.MkdirAll(absolute, 0o700); err != nil {
		return Uploads{}, fmt.Errorf("create upload directory: %w", err)
	}
	return Uploads{directory: absolute}, nil
}

// Source is the absolute source tree of a completed upload.
func (u Uploads) Source(id string) (string, error) {
	if err := validUploadID(id); err != nil {
		return "", err
	}
	return filepath.Join(u.directory, id, "files"), nil
}

// Create records the declared files and materializes empty ones, which are complete at once.
func (u Uploads) Create(id string, files []model.UploadFileInput) error {
	if err := validUploadID(id); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(u.directory, id), 0o700); err != nil {
		return fmt.Errorf("create upload: %w", err)
	}
	root, err := os.OpenRoot(filepath.Join(u.directory, id))
	if err != nil {
		return fmt.Errorf("open upload: %w", err)
	}
	defer closeRoot(root)
	for _, name := range []string{"files", "parts"} {
		if err = root.Mkdir(name, 0o700); err != nil {
			return fmt.Errorf("create upload directory: %w", err)
		}
	}
	manifest, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("encode upload manifest: %w", err)
	}
	if err = writeSynced(root, uploadManifest, manifest); err != nil {
		return err
	}
	for _, file := range files {
		if file.SizeBytes == 0 {
			if err = publishPart(root, file.Path, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// Files returns the declared files and the bytes received for each.
func (u Uploads) Files(id string) ([]model.UploadFileInput, []int64, error) {
	root, err := u.open(id)
	if err != nil {
		return nil, nil, err
	}
	defer closeRoot(root)
	files, err := readManifest(root)
	if err != nil {
		return nil, nil, err
	}
	received := make([]int64, len(files))
	for index, file := range files {
		if received[index], err = receivedBytes(root, index, file); err != nil {
			return nil, nil, err
		}
	}
	return files, received, nil
}

// Append writes reader at offset of file index and publishes the file into files/ once it
// reaches its declared size. It returns the received byte count.
func (u Uploads) Append(id string, index int, offset int64, reader io.Reader) (int64, error) {
	root, err := u.open(id)
	if err != nil {
		return 0, err
	}
	defer closeRoot(root)
	files, err := readManifest(root)
	if err != nil {
		return 0, err
	}
	if index < 0 || index >= len(files) {
		return 0, model.ErrNotFound
	}
	file := files[index]
	received, err := receivedBytes(root, index, file)
	if err != nil {
		return 0, err
	}
	if offset != received || received == file.SizeBytes {
		return received, model.ErrUploadOffset
	}
	part := partName(index)
	target, err := root.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return 0, fmt.Errorf("open upload part: %w", err)
	}
	defer closeFile(target)
	// A chunk is all or nothing: an interrupted or oversized body leaves the part at the
	// client's offset, so a retry resends exactly that chunk.
	written, err := io.Copy(target, io.LimitReader(reader, file.SizeBytes-received+1))
	if err != nil {
		return offset, errors.Join(fmt.Errorf("write upload part: %w", err), target.Truncate(offset))
	}
	received += written
	if received > file.SizeBytes {
		return offset, errors.Join(model.ErrInvalid, target.Truncate(offset))
	}
	if err = target.Sync(); err != nil {
		return received, fmt.Errorf("sync upload part: %w", err)
	}
	if received == file.SizeBytes {
		return received, publishPart(root, file.Path, &part)
	}
	return received, nil
}

func (u Uploads) Remove(id string) error {
	if err := validUploadID(id); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(u.directory, id)); err != nil {
		return fmt.Errorf("remove upload: %w", err)
	}
	return nil
}

// IDs lists the staged upload IDs, each with the last time its directory changed.
func (u Uploads) IDs() (map[string]time.Time, error) {
	entries, err := os.ReadDir(u.directory)
	if err != nil {
		return nil, fmt.Errorf("list uploads: %w", err)
	}
	result := make(map[string]time.Time, len(entries))
	for _, entry := range entries {
		if validUploadID(entry.Name()) != nil || !entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if errors.Is(infoErr, os.ErrNotExist) {
			continue
		}
		if infoErr != nil {
			return nil, fmt.Errorf("inspect upload: %w", infoErr)
		}
		result[entry.Name()] = info.ModTime()
	}
	return result, nil
}

// FreeBytes reports the space available to the upload directory's filesystem.
func (u Uploads) FreeBytes() (int64, error) {
	var status syscall.Statfs_t
	if err := syscall.Statfs(u.directory, &status); err != nil {
		return 0, fmt.Errorf("read upload filesystem: %w", err)
	}
	return int64(status.Bavail) * status.Bsize, nil
}

func (u Uploads) open(id string) (*os.Root, error) {
	if err := validUploadID(id); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Join(u.directory, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open upload: %w", err)
	}
	return root, nil
}

func readManifest(root *os.Root) ([]model.UploadFileInput, error) {
	data, err := root.ReadFile(uploadManifest)
	if err != nil {
		return nil, fmt.Errorf("read upload manifest: %w", err)
	}
	var files []model.UploadFileInput
	if err = json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("decode upload manifest: %w", err)
	}
	return files, nil
}

func receivedBytes(root *os.Root, index int, file model.UploadFileInput) (int64, error) {
	if _, err := root.Stat(path.Join("files", file.Path)); err == nil {
		return file.SizeBytes, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("inspect upload file: %w", err)
	}
	info, err := root.Stat(partName(index))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inspect upload part: %w", err)
	}
	return info.Size(), nil
}

// publishPart moves a completed part (or an empty file when part is nil) to files/<name>.
func publishPart(root *os.Root, name string, part *string) error {
	target := path.Join("files", name)
	if err := root.MkdirAll(path.Dir(target), 0o700); err != nil {
		return fmt.Errorf("create upload file directory: %w", err)
	}
	if part == nil {
		return writeSynced(root, target, nil)
	}
	if err := root.Rename(*part, target); err != nil {
		return fmt.Errorf("publish upload file: %w", err)
	}
	return nil
}

func writeSynced(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create upload file: %w", err)
	}
	defer closeFile(file)
	if _, err = file.Write(data); err != nil {
		return fmt.Errorf("write upload file: %w", err)
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("sync upload file: %w", err)
	}
	return nil
}

func partName(index int) string { return "parts/" + strconv.Itoa(index) }

func validUploadID(id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return model.ErrInvalid
	}
	return nil
}

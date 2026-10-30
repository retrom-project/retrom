package storage

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"path"
	"sort"
	"time"

	"retrom/internal/model"

	"github.com/google/uuid"
)

const (
	MaximumMembers           = model.MaximumContentMembers
	MaximumContentSize int64 = model.MaximumContentSize
	// Two names, local/central headers, ZIP64 extras and descriptors per member.
	MaximumPackageSize = model.MaximumPackageSize
)

// Stage copies bounded input into this operation's temporary tree, never Game storage.
func (s *Store) Stage(ctx context.Context, operation string, reader io.Reader) (File, error) {
	return s.StageLimit(ctx, operation, reader, MaximumFileSize)
}

func (s *Store) StageLimit(ctx context.Context, operation string, reader io.Reader, maximum int64) (File, error) {
	if maximum < 0 || maximum > MaximumFileSize {
		return File{}, model.ErrInvalid
	}
	return s.stageLimit(ctx, operation, reader, maximum)
}

func (s *Store) stageLimit(ctx context.Context, operation string, reader io.Reader, maximum int64) (File, error) {
	if !model.UUID(operation) {
		return File{}, model.ErrInvalid
	}
	free, err := s.FreeBytes()
	if err != nil {
		return File{}, err
	}
	if free < 1024*1024*1024 {
		return File{}, model.ErrStorageFull
	}
	spaceBound := free-1024*1024*1024 < maximum
	maximum = min(maximum, free-1024*1024*1024)
	dir := "temporary/" + operation
	if err := s.root.MkdirAll(dir, 0o700); err != nil {
		return File{}, fmt.Errorf("create staging: %w", err)
	}
	key := dir + "/" + uuid.NewString()
	f, err := s.copyPayload(ctx, key, reader, maximum, false)
	f.Key = key
	if spaceBound && errors.Is(err, errTooLarge) {
		// The free-space reserve, not the file size limit, stopped this copy.
		return f, fmt.Errorf("staged file exceeds free space: %w", model.ErrStorageFull)
	}
	return f, err
}

// PublishStaged moves a complete independent file without copying its bytes.
func (s *Store) PublishStaged(id string, source model.ContentFile) (model.GameFile, error) {
	owner, err := OwnerKey("games", id)
	if err != nil {
		return model.GameFile{}, err
	}
	if !SafeRelative(source.StorageKey) || source.Offset != 0 {
		return model.GameFile{}, model.ErrInvalid
	}
	file, err := s.Read(source.StorageKey)
	if err != nil {
		return model.GameFile{}, err
	}
	err = errors.Join(file.Sync(), file.Close())
	if err != nil {
		return model.GameFile{}, fmt.Errorf("sync staged standalone file: %w", err)
	}
	key := owner + "/" + uuid.NewString()
	if err = s.publish(source.StorageKey, key, owner); err != nil {
		return model.GameFile{}, errors.Join(err, s.Remove(key))
	}
	f := model.GameFile{
		ID: uuid.NewString(), LogicalKey: source.LogicalKey, StorageKey: key, Role: "content",
		SHA256: source.SHA256, SizeBytes: source.SizeBytes,
	}
	return f, nil
}

func ValidateContent(files []model.ContentFile) error {
	if len(files) == 0 || len(files) > MaximumMembers {
		return model.ErrInvalid
	}
	seen := make(map[string]bool, len(files))
	var total int64
	for _, f := range files {
		if !SafeRelative(f.LogicalKey) || seen[f.LogicalKey] || !model.Hash(f.SHA256) || f.SizeBytes < 0 ||
			f.SizeBytes > MaximumFileSize {
			return model.ErrInvalid
		}
		total += f.SizeBytes
		if total > MaximumContentSize {
			return errTooLarge
		}
		seen[f.LogicalKey] = true
	}
	return nil
}

// BuildPackage verifies both its inputs and the complete persisted ZIP before publication.
func (s *Store) BuildPackage(ctx context.Context, id string, files []model.ContentFile,
) (model.GameFile, error) {
	return s.buildPackage(ctx, id, files, false)
}

// BuildTemporaryPackage owns a Run resource below runs/<run-id>. Source bytes are hashed while
// copied; the disposable copy skips the durable read-back and fsync of Game packages.
func (s *Store) BuildTemporaryPackage(ctx context.Context, id string, files []model.ContentFile,
) (model.GameFile, error) {
	return s.buildPackage(ctx, id, files, true)
}

func (s *Store) buildPackage(ctx context.Context, id string, files []model.ContentFile,
	temporary bool,
) (model.GameFile, error) {
	if err := ValidateContent(files); err != nil {
		return model.GameFile{}, err
	}
	if err := s.packageCapacity(files); err != nil {
		return model.GameFile{}, err
	}
	kind := "games"
	if temporary {
		kind = "runs"
	}
	owner, err := OwnerKey(kind, id)
	if err != nil {
		return model.GameFile{}, err
	}
	key, _, err := s.TemporaryPath()
	if err != nil {
		return model.GameFile{}, err
	}
	defer s.removePackageTemporary(key)
	f, err := s.root.OpenFile(key, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return model.GameFile{}, diskFailure("create package", err)
	}
	defer closeFile(f)
	files = append([]model.ContentFile(nil), files...)
	sort.Slice(files, func(i, j int) bool { return files[i].LogicalKey < files[j].LogicalKey })
	written, err := s.writePackage(ctx, f, files)
	if err != nil {
		return model.GameFile{}, err
	}
	members, err := verifyPackage(ctx, f, files, written.SizeBytes, !temporary)
	if err != nil {
		return model.GameFile{}, err
	}
	if !temporary {
		if err = f.Sync(); err != nil {
			return model.GameFile{}, diskFailure("sync package", err)
		}
	}
	destination := owner + "/" + uuid.NewString()
	if err = s.publish(key, destination, owner); err != nil {
		return model.GameFile{}, errors.Join(err, s.Remove(destination))
	}
	return model.GameFile{
		ID: uuid.NewString(), LogicalKey: "game.zip", Role: "project_package", StorageKey: destination,
		SizeBytes: written.SizeBytes, SHA256: written.SHA256, Members: members, MemberCount: len(members),
	}, nil
}

// writePackage measures the physical object in the same pass that writes it.
func (s *Store) writePackage(ctx context.Context, f *os.File, files []model.ContentFile) (Digest, error) {
	hash, counter := sha256.New(), &byteCounter{}
	writer := zip.NewWriter(io.MultiWriter(f, hash, counter))
	for _, source := range files {
		if err := s.writeMember(ctx, writer, source); err != nil {
			return Digest{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return Digest{}, diskFailure("close package", err)
	}
	if counter.size > MaximumPackageSize {
		return Digest{}, errTooLarge
	}
	return Digest{SizeBytes: counter.size, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

type byteCounter struct{ size int64 }

func (c *byteCounter) Write(buffer []byte) (int, error) {
	c.size += int64(len(buffer))
	return len(buffer), nil
}

func (s *Store) packageCapacity(files []model.ContentFile) error {
	var total int64
	for _, file := range files {
		total += file.SizeBytes
	}
	free, err := s.FreeBytes()
	if err != nil {
		return err
	}
	if free < total+int64(len(files))*(2*1024+256)+1024+1024*1024*1024 {
		return model.ErrStorageFull
	}
	return nil
}

func (s *Store) writeMember(ctx context.Context, writer *zip.Writer, source model.ContentFile) error {
	reader, err := s.OpenContent(source.StorageKey, source.Offset, source.SizeBytes)
	if err != nil {
		return err
	}
	defer closeFile(reader)
	header := &zip.FileHeader{
		Name: source.LogicalKey, Method: zip.Store,
		Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	member, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create package member: %w", err)
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(member, hash), &contextReader{ctx: ctx, reader: reader})
	if err != nil {
		return diskFailure("write package member", err)
	}
	if size != source.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != source.SHA256 {
		return fmt.Errorf("content changed: %w", model.ErrInvalid)
	}
	return nil
}

// verifyPackage always checks the persisted directory; readBack also rehashes every member.
func verifyPackage(ctx context.Context, file *os.File, sources []model.ContentFile, written int64,
	readBack bool,
) ([]model.GameFileMember, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat package: %w", err)
	}
	if info.Size() != written {
		return nil, fmt.Errorf("package size changed: %w", model.ErrInvalid)
	}
	archive, err := zip.NewReader(file, info.Size())
	if err != nil {
		return nil, fmt.Errorf("read package: %w", err)
	}
	if len(archive.File) != len(sources) {
		return nil, model.ErrInvalid
	}
	result := make([]model.GameFileMember, 0, len(sources))
	for i, member := range archive.File {
		m, verifyErr := verifyMember(ctx, member, sources[i], info.Size(), readBack)
		if verifyErr != nil {
			return nil, verifyErr
		}
		result = append(result, m)
	}
	return result, nil
}

func verifyMember(ctx context.Context, member *zip.File, source model.ContentFile,
	objectSize int64, readBack bool,
) (model.GameFileMember, error) {
	offset, err := member.DataOffset()
	if err != nil || member.Method != zip.Store || member.Name != source.LogicalKey ||
		member.UncompressedSize64 != uint64(source.SizeBytes) {
		return model.GameFileMember{}, model.ErrInvalid
	}
	if offset < 0 || offset > objectSize || source.SizeBytes > objectSize-offset {
		return model.GameFileMember{}, model.ErrInvalid
	}
	if readBack {
		if err = verifyMemberBytes(ctx, member, source); err != nil {
			return model.GameFileMember{}, err
		}
	}
	media := mime.TypeByExtension(path.Ext(source.LogicalKey))
	if media == "" {
		media = "application/octet-stream"
	}
	return model.GameFileMember{
		LogicalPath: source.LogicalKey, DataOffset: offset, SizeBytes: source.SizeBytes,
		SHA256: source.SHA256, MediaType: media,
	}, nil
}

func verifyMemberBytes(ctx context.Context, member *zip.File, source model.ContentFile) error {
	reader, err := member.Open()
	if err != nil {
		return fmt.Errorf("open package member: %w", err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, &contextReader{ctx: ctx, reader: reader})
	closeErr := reader.Close()
	if copyErr != nil {
		return fmt.Errorf("verify package member: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close package member: %w", closeErr)
	}
	if size != source.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != source.SHA256 {
		return model.ErrInvalid
	}
	return nil
}

func (s *Store) removePackageTemporary(key string) {
	if err := s.Remove(key); err != nil {
		slog.Error("remove package temporary", "error", err)
	}
}

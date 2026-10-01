package launch

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"retrom/internal/cleanup"
	application "retrom/internal/service/launch"
	"retrom/internal/zipentry"
)

// WriteDependencyBundle is the shared wire format for config metadata and HTTP delivery.
// The caller supplies only members already authorized by the frozen launch snapshot.
func WriteDependencyBundle(
	ctx context.Context, output io.Writer, files []BundleFile, open func(string) (*os.File, error),
) error {
	if _, err := BundleIdentity(files); err != nil {
		return err
	}
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(left, right BundleFile) int {
		return strings.Compare(left.LogicalName, right.LogicalName)
	})
	archive := zip.NewWriter(output)
	for _, entry := range ordered {
		if err := writeDependencyMember(ctx, archive, entry, open); err != nil {
			cleanup.Error("close incomplete dependency bundle", archive.Close())
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("close dependency bundle: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("finish dependency bundle: %w", err)
	}
	return nil
}

func writeDependencyMember(
	ctx context.Context, archive *zip.Writer, entry BundleFile, open func(string) (*os.File, error),
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("begin dependency member: %w", err)
	}
	source, err := open(entry.FileRecord)
	if err != nil {
		return fmt.Errorf("open dependency member: %w", err)
	}
	defer func() { cleanup.Error("close dependency member", source.Close()) }()
	destination, err := archive.CreateHeader(zipentry.StoreHeader(entry.LogicalName))
	if err != nil {
		return fmt.Errorf("create dependency member: %w", err)
	}
	digest := sha256.New()
	if _, err := io.Copy(contextBundleWriter{ctx: ctx, output: io.MultiWriter(destination, digest)}, source); err != nil {
		return fmt.Errorf("write dependency member: %w", err)
	}
	if hex.EncodeToString(digest.Sum(nil)) != entry.SHA256 {
		return ErrBlocked
	}
	return nil
}

type contextBundleWriter struct {
	ctx    context.Context
	output io.Writer
}

func (writer contextBundleWriter) Write(data []byte) (int, error) {
	if err := writer.ctx.Err(); err != nil {
		return 0, fmt.Errorf("write canceled dependency bundle: %w", err)
	}
	n, err := writer.output.Write(data)
	if err != nil {
		return n, fmt.Errorf("write dependency bundle: %w", err)
	}
	return n, nil
}

type bundleCounter struct {
	output io.Writer
	size   int64
}

func (writer *bundleCounter) Write(data []byte) (int, error) {
	n, err := writer.output.Write(data)
	writer.size += int64(n)
	if err != nil {
		return n, fmt.Errorf("count dependency bundle: %w", err)
	}
	return n, nil
}

func (source *Sources) DescribeBundle(
	ctx context.Context, files []application.ConfigFile,
) (application.ConfigArchive, error) {
	members := make([]BundleFile, 0, len(files))
	for _, file := range files {
		members = append(members, BundleFile{LogicalName: file.LogicalName, SHA256: file.Digest, FileRecord: file.FileRecord})
	}
	if source.blobs == nil {
		return application.ConfigArchive{}, ErrBlocked
	}
	digest := sha256.New()
	output := &bundleCounter{output: digest}
	if err := WriteDependencyBundle(ctx, output, members, source.blobs.OpenRecord); err != nil {
		return application.ConfigArchive{}, err
	}
	return application.ConfigArchive{SHA256: hex.EncodeToString(digest.Sum(nil)), SizeBytes: output.size}, nil
}

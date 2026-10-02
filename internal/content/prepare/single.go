// Package prepare owns content normalization independently of import and publication workflows.
package prepare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	contentprofile "retrom/internal/content/profile"
	"retrom/internal/filestore"
	"retrom/internal/importing"
)

type Files interface {
	OpenRecord(string) (*os.File, error)
	Path(string) string
	Put(io.Reader) (filestore.Metadata, error)
}

type File struct {
	LogicalName, Record, SHA256 string
	Size                        int64
}

type Single struct {
	File           File
	ArchiveEntries []importing.ArchiveEntry
	Selected       *importing.ArchiveEntry
	Materialized   *filestore.Metadata
}

type Invalid struct{ Code string }

func (err *Invalid) Error() string { return err.Code }

type Service struct{ files Files }

func New(files Files) *Service { return &Service{files: files} }

func (service *Service) Single(ctx context.Context, platformID string, source File) (Single, error) {
	if err := ctx.Err(); err != nil {
		return Single{}, fmt.Errorf("prepare content: %w", err)
	}
	profile, exists := contentprofile.ByPlatform(platformID)
	if !exists {
		return Single{}, &Invalid{Code: "UNSUPPORTED_CONTENT_FORMAT"}
	}
	result := Single{File: source}
	if !contentprofile.AcceptsRaw(platformID, source.LogicalName) {
		var err error
		result, err = service.archive(ctx, platformID, profile, source)
		if err != nil {
			return Single{}, err
		}
	}
	if err := service.validate(ctx, platformID, result.File); err != nil {
		return Single{}, err
	}
	return result, nil
}

func (service *Service) archive(ctx context.Context, platformID string,
	profile contentprofile.Profile, source File,
) (Single, error) {
	format, code := ArchiveFormat(source.LogicalName)
	if code != "" {
		return Single{}, &Invalid{Code: code}
	}
	if profile.ArchivePolicy != contentprofile.ArchiveSinglePrimary ||
		!contentprofile.AcceptsArchive(platformID, format) {
		return Single{}, &Invalid{Code: "UNSUPPORTED_CONTENT_FORMAT"}
	}
	entries, err := ScanArchive(ctx, service.files.Path(source.Record), format)
	if err != nil {
		return Single{}, archiveError(err)
	}
	selected, err := contentprofile.SelectArchivePrimary(platformID, entries)
	if errors.Is(err, contentprofile.ErrNoSupportedContent) {
		return Single{}, &Invalid{Code: "NO_SUPPORTED_CONTENT"}
	}
	if errors.Is(err, contentprofile.ErrAmbiguousPrimaryContent) {
		return Single{}, &Invalid{Code: "AMBIGUOUS_PRIMARY_CONTENT"}
	}
	if err != nil {
		return Single{}, fmt.Errorf("select archive content: %w", err)
	}
	materialized, err := Materialize(ctx, service.files, service.files.Path(source.Record), selected)
	if err != nil {
		return Single{}, archiveError(err)
	}
	return Single{
		File: File{
			LogicalName: filepath.Base(selected.NormalizedPath), Record: materialized.Record,
			SHA256: materialized.SHA256, Size: materialized.Size,
		},
		ArchiveEntries: entries, Selected: &selected, Materialized: &materialized,
	}, nil
}

func ArchiveFormat(name string) (contentprofile.ArchiveFormat, string) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip":
		return contentprofile.ArchiveZIP, ""
	case ".7z":
		return contentprofile.ArchiveSevenZip, ""
	default:
		if strings.HasSuffix(strings.ToLower(name), ".7z.001") {
			return "", "ARCHIVE_VOLUME_UNSUPPORTED"
		}
		return "", "UNSUPPORTED_CONTENT_FORMAT"
	}
}

func ScanArchive(ctx context.Context, path string, format contentprofile.ArchiveFormat,
) ([]importing.ArchiveEntry, error) {
	var entries []importing.ArchiveEntry
	var err error
	if format == contentprofile.ArchiveZIP {
		entries, err = importing.ScanZIP(ctx, path, importing.DefaultArchiveLimits())
	} else {
		entries, err = importing.ScanSevenZip(ctx, path, importing.DefaultArchiveLimits())
	}
	if err != nil {
		return nil, fmt.Errorf("scan content archive: %w", err)
	}
	return entries, nil
}

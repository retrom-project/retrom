// Package archive owns ZIP, appended NW.js ZIP and Electron ASAR resources.
package archive

import (
	"context"
	"errors"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
	"retrom/internal/model/diagnostics"
	librarymodel "retrom/internal/model/libraryimport"
)

var errCursorState = errors.New("archive: invalid cursor state")

type Factory struct{ reporter diagnostics.ErrorReporter }

func New(reporter diagnostics.ErrorReporter) *Factory {
	if reporter == nil {
		panic("archive: diagnostic reporter is required")
	}
	return &Factory{reporter: reporter}
}

func (factory *Factory) OpenProject(
	ctx context.Context, path string, format contentprofile.ArchiveFormat, limits importing.ArchiveLimits,
) (librarymodel.ArchiveReader, error) {
	switch format {
	case contentprofile.ArchiveZIP:
		cursor, err := factory.openZIP(ctx, path, limits)
		if err != nil {
			return nil, err
		}
		return cursor, nil
	case contentprofile.ArchiveNWJSExecutable:
		if err := factory.ValidateNWJSExecutable(ctx, path); err != nil {
			return nil, err
		}
		cursor, err := factory.openZIP(ctx, path, limits)
		if err != nil {
			return nil, err
		}
		return cursor, nil
	case contentprofile.ArchiveElectronASAR:
		cursor, err := factory.openASAR(ctx, path, limits)
		if err != nil {
			return nil, err
		}
		return cursor, nil
	case contentprofile.ArchiveSevenZip:
		return nil, importing.ErrArchiveMethodUnsupported
	}
	return nil, importing.ErrArchiveMethodUnsupported
}

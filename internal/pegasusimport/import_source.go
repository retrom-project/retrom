package pegasusimport

import (
	"context"
	"fmt"

	"retrom/internal/blobstore"
	application "retrom/internal/service/pegasusimport"
)

type importSource struct {
	service *Sources
	root    Root
}

func (source importSource) CopyFile(
	ctx context.Context, unit application.Work, file application.ExecutionFile,
) (application.VerifiedBlob, error) {
	blob, err := source.service.copySource(ctx, source.root, unit.RelativePath, file.Path, file.Size, file.Facts)
	if err != nil {
		return application.VerifiedBlob{}, fmt.Errorf("read Pegasus import file: %w", err)
	}
	return verifiedMaterial(blob), nil
}

func (source importSource) CopyAsset(
	ctx context.Context, unit application.Work, asset application.ExecutionAsset,
) (application.VerifiedBlob, bool, error) {
	blob, valid, err := source.service.copyAsset(ctx, source.root, unit.RelativePath, asset)
	if err != nil {
		return application.VerifiedBlob{}, valid, fmt.Errorf("read Pegasus import asset: %w", err)
	}
	return verifiedMaterial(blob), valid, nil
}

func verifiedMaterial(metadata blobstore.Metadata) application.VerifiedBlob {
	return application.VerifiedBlob{
		SHA256: metadata.SHA256,
		MD5:    metadata.MD5,
		SHA1:   metadata.SHA1,
		CRC32:  metadata.CRC32,
		Size:   metadata.Size,
	}
}

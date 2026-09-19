package pegasusimport

import (
	"context"
	"fmt"

	blobmodel "retrom/internal/model/blob"
	pegasusimportmodel "retrom/internal/model/pegasusimport"
)

type importSource struct {
	service *Sources
	root    Root
}

func (source importSource) CopyFile(
	ctx context.Context, unit pegasusimportmodel.Work, file pegasusimportmodel.ExecutionFile,
) (pegasusimportmodel.VerifiedBlob, error) {
	blob, err := source.service.copySource(ctx, source.root, unit.RelativePath, file.Path, file.Size, file.Facts)
	if err != nil {
		return pegasusimportmodel.VerifiedBlob{}, fmt.Errorf("read Pegasus import file: %w", err)
	}
	return verifiedMaterial(blob), nil
}

func (source importSource) CopyAsset(
	ctx context.Context, unit pegasusimportmodel.Work, asset pegasusimportmodel.ExecutionAsset,
) (pegasusimportmodel.VerifiedBlob, bool, error) {
	blob, valid, err := source.service.copyAsset(ctx, source.root, unit.RelativePath, asset)
	if err != nil {
		return pegasusimportmodel.VerifiedBlob{}, valid, fmt.Errorf("read Pegasus import asset: %w", err)
	}
	return verifiedMaterial(blob), valid, nil
}

func verifiedMaterial(metadata blobmodel.PreparedBlob) pegasusimportmodel.VerifiedBlob {
	return pegasusimportmodel.VerifiedBlob{
		SHA256: metadata.SHA256,
		MD5:    metadata.MD5,
		SHA1:   metadata.SHA1,
		CRC32:  metadata.CRC32,
		Size:   metadata.Size,
	}
}

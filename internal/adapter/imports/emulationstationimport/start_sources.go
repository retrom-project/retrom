package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"retrom/internal/adapter/files/serversource"
	"retrom/internal/foundation/cleanup"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
)

func (source *Sources) VerifyGamelists(
	ctx context.Context, rootID, path string, evidence []emulationstationimportmodel.GamelistEvidence,
) error {
	root, ok := source.roots[rootID]
	if !ok {
		return emulationstationimportmodel.ErrSourceChanged
	}
	for _, value := range evidence {
		if err := verifyStartGamelist(ctx, root, path, value); err != nil {
			return err
		}
	}
	return nil
}

func verifyStartGamelist(
	ctx context.Context,
	root Root,
	path string,
	expected emulationstationimportmodel.GamelistEvidence,
) error {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return fmt.Errorf("acquire EmulationStation start reader: %w", err)
	}
	defer release()
	file, before, err := serversource.OpenRelativeFile(root.path, path, expected.RelativePath)
	if err != nil {
		return fmt.Errorf("%w: %w", emulationstationimportmodel.ErrSourceChanged, err)
	}
	defer func() { cleanup.Error("close EmulationStation start source", file.Close()) }()
	if before.Size() != expected.SizeBytes || serversource.FactsDigest(before) != expected.FactsDigest {
		return emulationstationimportmodel.ErrSourceChanged
	}
	if expected.ContentDigest == nil {
		return nil
	}
	if expected.SizeBytes < 0 || expected.SizeBytes > emulationstationimportmodel.MaxSnapshotGamelistBytes {
		return emulationstationimportmodel.ErrSourceChanged
	}
	digest := sha256.New()
	reader := &contextReader{ctx: ctx, reader: io.LimitReader(file, expected.SizeBytes+1)}
	count, err := io.Copy(digest, reader)
	if err != nil {
		return fmt.Errorf("%w: %w", emulationstationimportmodel.ErrSourceChanged, err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", emulationstationimportmodel.ErrSourceChanged, err)
	}
	after, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%w: %w", emulationstationimportmodel.ErrSourceChanged, err)
	}
	if count != expected.SizeBytes || !serversource.SameFileFacts(before, after) ||
		hex.EncodeToString(digest.Sum(nil)) != *expected.ContentDigest {
		return emulationstationimportmodel.ErrSourceChanged
	}
	return nil
}

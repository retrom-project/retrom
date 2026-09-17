package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	model "retrom/internal/model/launch"
	"slices"
	"strings"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
)

const (
	RuntimeProjectContentPrefix = "/runtime/content/project/"
	MaximumProjectFiles         = 100_000
	MKXPArchiveName             = "__retrom__/game.mkxpz"
	MKXPArchivePublicName       = "game.mkxpz"
)

func ProjectIdentity(files []model.ConfigFile) (string, error) {
	if len(files) == 0 || len(files) > MaximumProjectFiles {
		return "", model.ErrBlocked
	}
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(left, right model.ConfigFile) int {
		return strings.Compare(left.LogicalName, right.LogicalName)
	})
	digest := sha256.New()
	_, _ = digest.Write([]byte("RETROM_RUNTIME_PROJECT_V1\x00"))
	previous := ""
	expectedFormat := ordered[0].Format
	seen := make(map[string]struct{}, len(ordered))
	for _, file := range ordered {
		normalized, pathErr := importing.ValidateLogicalPath(file.LogicalName)
		folded := importing.ASCIICaseFold(normalized)
		_, duplicate := seen[folded]
		if pathErr != nil || normalized != file.LogicalName || previous == file.LogicalName || duplicate ||
			file.Format != expectedFormat || !validProjectContentFormat(file.Format) ||
			!validContentDigest(file.Digest) {
			return "", model.ErrBlocked
		}
		seen[folded] = struct{}{}
		_, _ = fmt.Fprintf(
			digest, "%d\x00%s\x00%d\x00%s\x00%s\x00",
			len(file.LogicalName), file.LogicalName, len(file.Format), file.Format, file.Digest,
		)
		previous = file.LogicalName
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func validProjectContentFormat(value string) bool {
	return contentprofile.IsProjectContentKind(contentprofile.ContentKind(value))
}

func RuntimeProjectContentRoot(identity string) (string, error) {
	if !validContentDigest(identity) {
		return "", model.ErrBlocked
	}
	return RuntimeProjectContentPrefix + identity + "/", nil
}

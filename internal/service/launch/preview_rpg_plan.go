package launch

import (
	"path"
	model "retrom/internal/model/launch"
	"strings"
	"unicode/utf8"

	"retrom/internal/capability/engine/rpgmaker/nativeweb"
)

const (
	rpgProjectFormat = "RPG_MAKER_PROJECT"
	rpgEasyIndexName = "__retrom__/index.json"
)

type rpgContentPlanBuilder struct {
	locked                                     []model.PreviewFile
	seen                                       map[string]struct{}
	projectFiles, nativeEntries, requiredFiles int
}

func RPGContentPolicy(deliveryProfile string) (string, bool, error) {
	switch deliveryProfile {
	case "FILE_TREE_PROJECT":
		return "RPG_EASYRPG_INDEX", false, nil
	case "SEEKABLE_PROJECT_ARCHIVE":
		return "RPG_MAKER_LAUNCH_BUNDLE", false, nil
	case "ISOLATED_WEB_PROJECT":
		return "", true, nil
	default:
		return "", false, model.ErrBlocked
	}
}

func RPGContentFiles(
	files []model.PreviewFile,
	requiredRole string,
	nativeRuntime bool,
) ([]model.PreviewFile, error) {
	if len(files) == 0 || len(files) > 10_006 {
		return nil, model.ErrBlocked
	}
	builder := rpgContentPlanBuilder{
		locked: make([]model.PreviewFile, 0, len(files)),
		seen:   make(map[string]struct{}, len(files)),
	}
	for _, file := range files {
		if err := builder.add(file, requiredRole, nativeRuntime); err != nil {
			return nil, model.ErrBlocked
		}
	}
	if builder.projectFiles == 0 || builder.projectFiles > 10_000 {
		return nil, model.ErrBlocked
	}
	if nativeRuntime && builder.nativeEntries != 1 {
		return nil, model.ErrBlocked
	}
	if requiredRole != "" && builder.requiredFiles != 1 {
		return nil, model.ErrBlocked
	}
	return builder.locked, nil
}

func (builder *rpgContentPlanBuilder) add(
	file model.PreviewFile,
	requiredRole string,
	nativeRuntime bool,
) error {
	logicalName, project, valid := rpgLockedLogicalName(file)
	if !valid {
		return model.ErrBlocked
	}
	if !includeRPGContentFile(project, nativeRuntime, logicalName) {
		return nil
	}
	if project {
		builder.projectFiles++
		if isNativeRPGEntry(nativeRuntime, logicalName) {
			builder.nativeEntries++
		}
	}
	if file.Role == requiredRole && requiredRole != "" {
		builder.requiredFiles++
	}
	if !validRPGProjectPath(logicalName) {
		return model.ErrBlocked
	}
	if _, duplicate := builder.seen[logicalName]; duplicate {
		return model.ErrBlocked
	}
	builder.seen[logicalName] = struct{}{}
	builder.locked = append(builder.locked, model.PreviewFile{
		BlobID: file.BlobID, LogicalName: logicalName,
	})
	return nil
}

func includeRPGContentFile(project, nativeRuntime bool, logicalName string) bool {
	return !project || !nativeRuntime || nativeweb.RuntimeFile(logicalName)
}

func isNativeRPGEntry(nativeRuntime bool, logicalName string) bool {
	return nativeRuntime && logicalName == "index.html"
}

func rpgLockedLogicalName(file model.PreviewFile) (string, bool, bool) {
	switch file.Role {
	case "PROJECT_FILE":
		return file.LogicalName, true, !strings.HasPrefix(file.LogicalName, "__retrom__/")
	case "RPG_EASYRPG_INDEX":
		return rpgEasyIndexName, false, true
	case "RPG_MAKER_LAUNCH_BUNDLE":
		return MKXPArchiveName, false, true
	default:
		return "", false, false
	}
}

func validRPGProjectPath(value string) bool {
	return value != "" && len(value) <= 512 && utf8.ValidString(value) &&
		path.Clean(value) == value && !path.IsAbs(value) &&
		value != "." && !strings.HasPrefix(value, "../") &&
		!strings.Contains(value, "\\") && !strings.ContainsRune(value, 0)
}

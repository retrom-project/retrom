package importing

import (
	"path"
	"strings"
)

type validatedElectronZIPItem struct {
	ordinal    int
	header     zipHeaderFacts
	path       string
	foldedPath string
}

type electronZIPLayout struct {
	appASAR  validatedElectronZIPItem
	unpacked map[string]validatedElectronZIPItem
}

func validateElectronZIPDirectory(headers []zipHeaderFacts, limits ArchiveLimits) ([]validatedElectronZIPItem, error) {
	if err := validateZIPEntryCount(len(headers), limits); err != nil {
		return nil, err
	}
	seenPath := make(map[string]struct{}, len(headers))
	seenFold := make(map[string]struct{}, len(headers))
	items := make([]validatedElectronZIPItem, 0, len(headers))
	var total int64
	for ordinal, item := range headers {
		pathValue, directory, err := validateZIPItem(item, limits, total)
		if err != nil {
			return nil, err
		}
		if directory {
			continue
		}
		expanded, ok := checkedArchiveSize(item.UncompressedSize64)
		if !ok {
			return nil, ErrArchiveLimitExceeded
		}
		total += expanded
		folded := ASCIICaseFold(pathValue)
		if err := recordArchivePath(seenPath, seenFold, pathValue, folded); err != nil {
			return nil, err
		}
		items = append(items, validatedElectronZIPItem{ordinal: ordinal, header: item, path: pathValue, foldedPath: folded})
	}
	return items, nil
}

func locateElectronZIPLayout(items []validatedElectronZIPItem) (electronZIPLayout, bool, error) {
	candidates := make([]validatedElectronZIPItem, 0, 1)
	for _, item := range items {
		if strings.EqualFold(path.Base(item.path), "app.asar") &&
			strings.EqualFold(path.Base(path.Dir(item.path)), "resources") {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return electronZIPLayout{}, false, nil
	}
	for _, candidate := range candidates {
		root := electronApplicationRoot(candidate.path)
		if hasElectronExecutable(items, root) {
			if len(candidates) != 1 {
				return electronZIPLayout{}, false, ErrElectronASARInvalid
			}
			return electronZIPLayout{
				appASAR: candidate, unpacked: electronUnpackedItems(items, candidate.path),
			}, true, nil
		}
	}
	return electronZIPLayout{}, false, nil
}

func electronApplicationRoot(appASARPath string) string {
	root := path.Dir(path.Dir(appASARPath))
	if root == "." {
		return ""
	}
	return root
}

func hasElectronExecutable(items []validatedElectronZIPItem, root string) bool {
	for _, item := range items {
		parent := path.Dir(item.path)
		if parent == "." {
			parent = ""
		}
		if parent == root && strings.EqualFold(path.Ext(item.path), ".exe") {
			return true
		}
	}
	return false
}

func electronUnpackedItems(
	items []validatedElectronZIPItem,
	appASARPath string,
) map[string]validatedElectronZIPItem {
	prefix := ASCIICaseFold(path.Join(path.Dir(appASARPath), "app.asar.unpacked")) + "/"
	result := make(map[string]validatedElectronZIPItem)
	for _, item := range items {
		if !strings.HasPrefix(item.foldedPath, prefix) {
			continue
		}
		relative := strings.TrimPrefix(item.path, item.path[:len(prefix)])
		result[ASCIICaseFold(relative)] = item
	}
	return result
}

func validateUnpackedASARMembers(members []asarMember, layout electronZIPLayout) error {
	for index := range members {
		if !members[index].unpacked {
			continue
		}
		item, exists := layout.unpacked[ASCIICaseFold(members[index].path)]
		if !exists {
			return ErrElectronASARInvalid
		}
		size, validSize := checkedArchiveSize(item.header.UncompressedSize64)
		if !validSize || size != members[index].size {
			return ErrElectronASARInvalid
		}
	}
	return nil
}

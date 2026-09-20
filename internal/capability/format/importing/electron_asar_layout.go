package importing

import (
	"path"
	"strings"
)

type ElectronZIPLayout struct {
	AppASAR  ZIPMember
	Unpacked map[string]ZIPMember
}

func LocateElectronZIPLayout(items []ZIPMember) (ElectronZIPLayout, bool, error) {
	candidates := make([]ZIPMember, 0, 1)
	for _, item := range items {
		if strings.EqualFold(path.Base(item.Path), "app.asar") &&
			strings.EqualFold(path.Base(path.Dir(item.Path)), "resources") {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return ElectronZIPLayout{}, false, nil
	}
	for _, candidate := range candidates {
		root := electronApplicationRoot(candidate.Path)
		if hasElectronExecutable(items, root) {
			if len(candidates) != 1 {
				return ElectronZIPLayout{}, false, ErrElectronASARInvalid
			}
			return ElectronZIPLayout{
				AppASAR: candidate, Unpacked: electronUnpackedItems(items, candidate.Path),
			}, true, nil
		}
	}
	return ElectronZIPLayout{}, false, nil
}

func electronApplicationRoot(appASARPath string) string {
	root := path.Dir(path.Dir(appASARPath))
	if root == "." {
		return ""
	}
	return root
}

func hasElectronExecutable(items []ZIPMember, root string) bool {
	for _, item := range items {
		parent := path.Dir(item.Path)
		if parent == "." {
			parent = ""
		}
		if parent == root && strings.EqualFold(path.Ext(item.Path), ".exe") {
			return true
		}
	}
	return false
}

func electronUnpackedItems(
	items []ZIPMember,
	appASARPath string,
) map[string]ZIPMember {
	prefix := ASCIICaseFold(path.Join(path.Dir(appASARPath), "app.asar.unpacked")) + "/"
	result := make(map[string]ZIPMember)
	for _, item := range items {
		if !strings.HasPrefix(item.FoldedPath, prefix) {
			continue
		}
		relative := strings.TrimPrefix(item.Path, item.Path[:len(prefix)])
		result[ASCIICaseFold(relative)] = item
	}
	return result
}

func ValidateUnpackedASARMembers(members []ASARMember, layout ElectronZIPLayout) error {
	for index := range members {
		if !members[index].Unpacked {
			continue
		}
		item, exists := layout.Unpacked[ASCIICaseFold(members[index].Path)]
		if !exists {
			return ErrElectronASARInvalid
		}
		size, validSize := checkedArchiveSize(item.Header.UncompressedSize64)
		if !validSize || size != members[index].Size {
			return ErrElectronASARInvalid
		}
	}
	return nil
}

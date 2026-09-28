package gamevariant

import (
	"path/filepath"
	"strings"
)

func mameContent(content Snapshot) bool {
	target := content.Source.TargetID
	if target != "mame-apple2" && target != "mame-atom" && target != "mame-pv1000" {
		return true
	}
	extension := strings.ToLower(filepath.Ext(content.Source.ValidationLogicalName))
	for _, file := range content.GameFiles {
		if file.LogicalName != content.Source.ValidationLogicalName {
			continue
		}
		return mameFile(target, extension, file.SizeBytes)
	}
	return false
}

func mameFile(target, extension string, size int64) bool {
	switch target {
	case "mame-apple2":
		return (extension == ".dsk" || extension == ".do") && size == 143360
	case "mame-atom":
		return extension == ".atm" && size > 22 && size <= 65557
	case "mame-pv1000":
		return (extension == ".bin" || extension == ".rom") &&
			(size == 8192 || size == 16384 || size == 32768)
	}
	return false
}

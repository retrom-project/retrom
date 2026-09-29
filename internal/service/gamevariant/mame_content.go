package gamevariant

import (
	"path/filepath"
	"strings"
)

func mameContent(content Snapshot) bool {
	target := content.Source.TargetID
	if target != "mame-apple2" && target != "mame-apple2e" && target != "mame-atom" &&
		target != "mame-pv1000" && target != "mame-sg1000" && target != "mame-coleco" {
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
	case "mame-apple2", "mame-apple2e":
		return (extension == ".dsk" || extension == ".do") && size == 143360
	case "mame-atom":
		return extension == ".atm" && size > 22 && size <= 65557
	case "mame-pv1000":
		return (extension == ".bin" || extension == ".rom") &&
			mamePVROMSize(size)
	case "mame-sg1000":
		return extension == ".sg" && mameCartridgeSize(size, 65536)
	case "mame-coleco":
		return extension == ".col" && mameCartridgeSize(size, 32768)
	}
	return false
}

func mamePVROMSize(size int64) bool {
	return size == 8192 || size == 16384 || size == 32768
}

func mameCartridgeSize(size, maximum int64) bool {
	return size >= 8192 && size <= maximum && size%8192 == 0
}

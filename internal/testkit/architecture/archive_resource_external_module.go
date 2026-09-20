package architecture

import (
	"crypto/sha256"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/tools/go/packages"
)

func (proof *archiveExternalProof) verifyModule(pkg *packages.Package) error {
	value := pkg.Module
	if value == nil || value.Main || value.Replace != nil || value.Error != nil || value.Dir == "" ||
		module.Check(value.Path, value.Version) != nil {
		return fmt.Errorf("%w: external declaration has no fixed module", errArchiveOrigin)
	}
	if proof.modules[value.Dir] != "" {
		return nil
	}
	if len(proof.modules) >= 8 {
		return fmt.Errorf("%w: external module proof limit", errArchiveOrigin)
	}
	expected, err := proof.moduleChecksum(value.Path, value.Version)
	if err != nil {
		return err
	}

	actual, err := dirhash.HashDir(value.Dir, value.Path+"@"+value.Version, dirhash.Hash1)
	if err != nil {
		return fmt.Errorf("%w: verify local module: %w", errArchiveOrigin, err)
	}
	if actual != expected {
		return fmt.Errorf("%w: local module differs from its fixed checksum", errArchiveOrigin)
	}
	proof.modules[value.Dir] = expected
	proof.origin.sources["go.mod"], proof.origin.sources["go.sum"] = true, true
	return nil
}

func (proof *archiveExternalProof) moduleChecksum(modulePath, version string) (string, error) {
	content, err := readInventoryFile(proof.origin.contract.root, "go.sum")
	if err != nil {
		return "", fmt.Errorf("%w: fixed module checksum: %w", errArchiveOrigin, err)
	}
	expected := ""
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == modulePath && fields[1] == version {
			if expected != "" && expected != fields[2] {
				return "", fmt.Errorf("%w: ambiguous module checksum", errArchiveOrigin)
			}
			expected = fields[2]
		}
	}
	if !strings.HasPrefix(expected, "h1:") {
		return "", fmt.Errorf("%w: module checksum is not pinned", errArchiveOrigin)
	}
	return expected, nil
}

func (proof *archiveExternalProof) noteExternal(pkg *packages.Package, position token.Pos) error {
	if err := proof.verifyModule(pkg); err != nil {
		return err
	}
	name := pkg.Fset.Position(position).Filename
	relative, err := filepath.Rel(pkg.Module.Dir, name)
	if err != nil || !safeRepositoryPath(filepath.ToSlash(relative)) {
		return fmt.Errorf("%w: declaration is outside its verified module", errArchiveOrigin)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return fmt.Errorf("%w: external source: %w", errArchiveOrigin, err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	if previous, exists := proof.sources[name]; exists && previous.SHA256 != sum {
		return fmt.Errorf("%w: external source changed during proof", errArchiveOrigin)
	}
	proof.sources[name] = ArchiveExternalSource{
		Module: pkg.Module.Path, Version: pkg.Module.Version, Path: filepath.ToSlash(relative), SHA256: sum,
	}
	return nil
}

func (proof *archiveExternalProof) snapshot() ([]ArchiveExternalSource, error) {
	result := make([]ArchiveExternalSource, 0, len(proof.sources))
	for name, source := range proof.sources {
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("%w: external snapshot: %w", errArchiveOrigin, err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != source.SHA256 {
			return nil, fmt.Errorf("%w: external source changed during proof", errArchiveOrigin)
		}
		result = append(result, source)
	}
	slices.SortFunc(result, func(left, right ArchiveExternalSource) int {
		return strings.Compare(left.Module+"/"+left.Path, right.Module+"/"+right.Path)
	})
	return result, nil
}

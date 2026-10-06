package requirements

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"

	"retrom/internal/content/diagnostic"
)

type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

var (
	digestPattern     = regexp.MustCompile(`^[a-f0-9]{64}$`)
	crcPattern        = regexp.MustCompile(`^[a-f0-9]{8}$`)
	machinePattern    = regexp.MustCompile(`^[a-z0-9_]+$`)
	ErrCatalogInvalid = errors.New("CONTENT_REQUIREMENT_CATALOG_INVALID")
)

func (asset Asset) Valid() bool {
	return safePath(asset.Path) && digestPattern.MatchString(asset.SHA256)
}

func safePath(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\\\x00") && !strings.HasPrefix(value, "/") &&
		path.Clean(value) == value &&
		value != ".." && !strings.HasPrefix(value, "../")
}

func validPlatform(value string) bool {
	return value == "naomi" || value == "naomi2" || value == "atomiswave"
}

type ROMFile struct {
	Name      string  `json:"name"`
	SizeBytes int64   `json:"sizeBytes"`
	CRC32     *string `json:"crc32"`
	Optional  bool    `json:"optional"`
}
type FlycastMachine struct {
	Name      string    `json:"name"`
	Parent    *string   `json:"parent"`
	Platform  string    `json:"platform"`
	MediaType string    `json:"mediaType"`
	Disc      *string   `json:"disc"`
	Files     []ROMFile `json:"files"`
}
type FlycastCore struct {
	Filename       string `json:"filename"`
	SHA256         string `json:"sha256"`
	SourceCommit   string `json:"sourceCommit"`
	TableSHA256    string `json:"tableSha256"`
	ExporterSHA256 string `json:"exporterSha256"`
}
type FlycastCatalog struct {
	SchemaVersion int              `json:"schemaVersion"`
	Kind          string           `json:"kind"`
	Core          FlycastCore      `json:"core"`
	Machines      []FlycastMachine `json:"machines"`
}
type ArchiveMember struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	CRC32     string `json:"crc32"`
}

func ParseFlycastCatalog(contents []byte, expected *Policy) (*FlycastCatalog, error) {
	if expected == nil || !expected.Valid() || expected.Kind != FlycastCartridge || len(contents) > 16<<20 {
		return nil, ErrCatalogInvalid
	}
	digest := sha256.Sum256(contents)
	if hex.EncodeToString(digest[:]) != expected.Catalog.SHA256 {
		return nil, ErrCatalogInvalid
	}
	var value FlycastCatalog
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || !validCatalogHeader(value) {
		return nil, ErrCatalogInvalid
	}
	if !validCatalogCore(value.Core, *expected.Core) {
		return nil, ErrCatalogInvalid
	}
	seen := map[string]bool{}
	for _, machine := range value.Machines {
		if seen[machine.Name] || !validMachine(machine) {
			return nil, ErrCatalogInvalid
		}
		seen[machine.Name] = true
	}
	return &value, nil
}

func validMachine(machine FlycastMachine) bool {
	if !machinePattern.MatchString(machine.Name) ||
		!slices.Contains([]string{"naomi", "naomi2", "atomiswave", "systemsp"}, machine.Platform) ||
		len(machine.Files) > 4096 {
		return false
	}
	if machine.Parent != nil && !machinePattern.MatchString(*machine.Parent) {
		return false
	}
	if !slices.Contains([]string{"CARTRIDGE", "GDROM", "COMPACT_FLASH"}, machine.MediaType) {
		return false
	}
	if (machine.MediaType != "CARTRIDGE") != (machine.Disc != nil) {
		return false
	}
	for _, file := range machine.Files {
		if !safePath(file.Name) || file.SizeBytes <= 0 || file.CRC32 != nil && !crcPattern.MatchString(*file.CRC32) {
			return false
		}
	}
	return true
}

func (policy *Policy) evaluateCartridge(facts Facts, name string) *diagnostic.Rejection {
	reject := func(code, relative string) *diagnostic.Rejection {
		return &diagnostic.Rejection{Code: code, RelativePath: relative}
	}
	if policy.CatalogFacts == nil {
		return reject("CONTENT_REQUIREMENTS_UNAVAILABLE", name)
	}
	machineName := strings.TrimSuffix(path.Base(name), path.Ext(name))
	var selected *FlycastMachine
	for index := range policy.CatalogFacts.Machines {
		machine := &policy.CatalogFacts.Machines[index]
		if machine.Name == machineName {
			selected = machine
			break
		}
	}
	if selected == nil {
		return reject("FLYCAST_MACHINE_UNKNOWN", name)
	}
	if selected.Platform != policy.Platform {
		return reject("FLYCAST_PLATFORM_MISMATCH", name)
	}
	if selected.MediaType != "CARTRIDGE" {
		return reject("FLYCAST_GDROM_UNSUPPORTED", name)
	}
	members := map[string]ArchiveMember{}
	checksums := map[string][]ArchiveMember{}
	for _, member := range facts.Archive {
		members[member.Name] = member
		checksums[member.CRC32] = append(checksums[member.CRC32], member)
	}
	for _, file := range selected.Files {
		if code := matchCartridgeFile(file, members, checksums); code != "" {
			return reject(code, name+"/"+file.Name)
		}
	}
	return nil
}

// Flycast resolves known CRCs before names. Equally checksummed members are safe
// only when all have the declared size, so archive ordering cannot change admission.
func matchCartridgeFile(file ROMFile, names map[string]ArchiveMember, checksums map[string][]ArchiveMember) string {
	if file.CRC32 != nil && len(checksums[*file.CRC32]) > 0 {
		for _, member := range checksums[*file.CRC32] {
			if member.SizeBytes != file.SizeBytes {
				return "FLYCAST_ROM_MISMATCH"
			}
		}
		return ""
	}
	member, exists := names[file.Name]
	if !exists {
		if file.Optional {
			return ""
		}
		return "FLYCAST_ARCHIVE_INCOMPLETE"
	}
	if member.SizeBytes != file.SizeBytes || file.CRC32 != nil && member.CRC32 != *file.CRC32 {
		return "FLYCAST_ROM_MISMATCH"
	}
	return ""
}

func validCatalogHeader(value FlycastCatalog) bool {
	return value.SchemaVersion == 1 && value.Kind == "FLYCAST_ROM_REQUIREMENTS" &&
		len(value.Machines) > 0 && len(value.Machines) <= 10000
}

func validCatalogCore(core FlycastCore, expected Asset) bool {
	return core.Filename == path.Base(expected.Path) && core.SHA256 == expected.SHA256 && len(core.SourceCommit) == 40 &&
		digestPattern.MatchString(core.TableSHA256) && digestPattern.MatchString(core.ExporterSHA256)
}

package arcadedat

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxDATBytes      = 64 << 20
	maxDirectiveSize = 64 << 10
	maxElements      = 500000
	maxDepth         = 32
	maxAttributes    = 64
	maxNameBytes     = 255
	maxFieldBytes    = 4096
)

var (
	ErrUnsafeDTD       = errors.New("DAT_UNSAFE_DTD")
	ErrLimitExceeded   = errors.New("DAT_LIMIT_EXCEEDED")
	ErrInvalidDocument = errors.New("DAT_INVALID_DOCUMENT")
)

type Stats struct {
	MachineCount                    int `json:"machine_count"`
	ROMEntryCount                   int `json:"rom_entry_count"`
	ROMEntryWithMergeCount          int `json:"rom_entry_with_merge_count"`
	ROMEntryWithBIOSCount           int `json:"rom_entry_with_bios_count"`
	ROMNodumpCount                  int `json:"rom_nodump_count"`
	ROMBaddumpCount                 int `json:"rom_baddump_count"`
	ROMMissingCRC32Count            int `json:"rom_missing_crc32_count"`
	ROMMissingSHA1Count             int `json:"rom_missing_sha1_count"`
	ROMMissingAllHashCount          int `json:"rom_missing_all_hash_count"`
	NonNodumpROMMissingAllHashCount int `json:"non_nodump_rom_missing_all_hash_count"`
	BIOSSetCount                    int `json:"bios_set_count"`
	DefaultBIOSSetCount             int `json:"default_bios_set_count"`
	DiskEntryCount                  int `json:"disk_entry_count"`
	DiskMissingSHA1Count            int `json:"disk_missing_sha1_count"`
	SampleEntryCount                int `json:"sample_entry_count"`
	CloneofRelationCount            int `json:"cloneof_relation_count"`
	RomofRelationCount              int `json:"romof_relation_count"`
	ExplicitBIOSMachineCount        int `json:"explicit_bios_machine_count"`
	BaseDependencyTargetCount       int `json:"base_dependency_target_count"`
	UnresolvedCloneofTargetCount    int `json:"unresolved_cloneof_target_count"`
	UnresolvedRomofTargetCount      int `json:"unresolved_romof_target_count"`
}

type machineInfo struct {
	cloneof string
	romof   string
	bios    bool
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func Parse(ctx context.Context, source io.Reader, coreID string) (Stats, error) {
	if coreID != "fbneo" && coreID != "mame2003" && coreID != "mame2003_plus" {
		return Stats{}, fmt.Errorf("%w: unsupported core", ErrInvalidDocument)
	}
	limited := &io.LimitedReader{R: source, N: maxDATBytes + 1}
	decoder := xml.NewDecoder(limited)
	decoder.Strict = true
	stats := Stats{}
	machines := make(map[string]machineInfo)
	baseTargets := make(map[string]struct{})
	rootSeen := false
	directiveSeen := false
	depth := 0
	elements := 0
	currentMachine := ""
	for {
		if err := ctx.Err(); err != nil {
			return Stats{}, fmt.Errorf("arcadedat/parser: %w", err)
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Stats{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
		}
		if limited.N <= 0 {
			return Stats{}, ErrLimitExceeded
		}
		switch value := token.(type) {
		case xml.Directive:
			if rootSeen || directiveSeen {
				return Stats{}, ErrUnsafeDTD
			}
			if err := validateDirective(value); err != nil {
				return Stats{}, ErrUnsafeDTD
			}
			directiveSeen = true
		case xml.ProcInst:
			if value.Target != "xml" || rootSeen {
				return Stats{}, ErrUnsafeDTD
			}
		case xml.StartElement:
			depth++
			elements++
			if depth > maxDepth || elements > maxElements || len(value.Attr) > maxAttributes {
				return Stats{}, ErrLimitExceeded
			}
			if !rootSeen {
				expected := "mame"
				if coreID == "fbneo" {
					expected = "datafile"
				}
				if value.Name.Local != expected || value.Name.Space != "" {
					return Stats{}, fmt.Errorf("%w: root", ErrInvalidDocument)
				}
				rootSeen = true
				continue
			}
			if value.Name.Space != "" {
				return Stats{}, fmt.Errorf("%w: namespace", ErrInvalidDocument)
			}
			switch value.Name.Local {
			case "machine", "game":
				name := attribute(value, "name")
				if name == "" || !validName(name) {
					return Stats{}, fmt.Errorf("%w: machine name", ErrInvalidDocument)
				}
				if _, exists := machines[name]; exists {
					return Stats{}, fmt.Errorf("%w: duplicate machine", ErrInvalidDocument)
				}
				info := machineInfo{
					cloneof: attribute(value, "cloneof"),
					romof:   attribute(value, "romof"),
					bios:    attribute(value, "isbios") == "yes",
				}
				machines[name] = info
				currentMachine = name
				stats.MachineCount++
				if info.cloneof != "" {
					stats.CloneofRelationCount++
				}
				if info.romof != "" {
					stats.RomofRelationCount++
				}
				if info.bios {
					stats.ExplicitBIOSMachineCount++
					baseTargets[name] = struct{}{}
				}
				if info.romof != "" && info.romof != info.cloneof {
					baseTargets[info.romof] = struct{}{}
				}
			case "rom":
				if currentMachine == "" {
					return Stats{}, fmt.Errorf("%w: rom outside machine", ErrInvalidDocument)
				}
				if err := countROM(value, &stats); err != nil {
					return Stats{}, err
				}
			case "disk":
				stats.DiskEntryCount++
				status := normalizedStatus(attribute(value, "status"))
				sha1Value := attribute(value, "sha1")
				if sha1Value == "" {
					stats.DiskMissingSHA1Count++
					if status != "nodump" {
						return Stats{}, fmt.Errorf("%w: disk hash", ErrInvalidDocument)
					}
				} else if !validHex(sha1Value, 40) {
					return Stats{}, fmt.Errorf("%w: disk sha1", ErrInvalidDocument)
				}
			case "biosset":
				stats.BIOSSetCount++
				if attribute(value, "default") == "yes" {
					stats.DefaultBIOSSetCount++
				}
			case "sample":
				stats.SampleEntryCount++
			}
		case xml.CharData:
			if len(value) > maxFieldBytes {
				return Stats{}, ErrLimitExceeded
			}
		case xml.EndElement:
			if (value.Name.Local == "machine" || value.Name.Local == "game") && depth == 2 {
				currentMachine = ""
			}
			depth--
			if depth < 0 {
				return Stats{}, ErrInvalidDocument
			}
		}
	}
	if !rootSeen || depth != 0 {
		return Stats{}, ErrInvalidDocument
	}
	unresolvedCloneof := make(map[string]struct{})
	unresolvedRomof := make(map[string]struct{})
	for _, info := range machines {
		if info.cloneof != "" {
			if _, exists := machines[info.cloneof]; !exists {
				unresolvedCloneof[info.cloneof] = struct{}{}
			}
		}
		if info.romof != "" {
			if _, exists := machines[info.romof]; !exists {
				unresolvedRomof[info.romof] = struct{}{}
			}
		}
	}
	stats.UnresolvedCloneofTargetCount = len(unresolvedCloneof)
	stats.UnresolvedRomofTargetCount = len(unresolvedRomof)
	stats.BaseDependencyTargetCount = len(baseTargets)
	return stats, nil
}

func validateDirective(value xml.Directive) error {
	if len(value) > maxDirectiveSize {
		return ErrLimitExceeded
	}
	text := strings.TrimSpace(string(value))
	upper := strings.ToUpper(text)
	if !strings.HasPrefix(upper, "DOCTYPE ") || strings.Contains(upper, "<!ENTITY") || strings.Contains(text, "%") {
		return ErrUnsafeDTD
	}
	return nil
}

func attribute(element xml.StartElement, name string) string {
	for _, item := range element.Attr {
		if item.Name.Space == "" && item.Name.Local == name {
			return item.Value
		}
	}
	return ""
}

//nolint:gocyclo // Contract branches stay contiguous for a single auditable decision.
func countROM(element xml.StartElement, stats *Stats) error {
	name := attribute(element, "name")
	if !validName(name) {
		return fmt.Errorf("%w: rom name", ErrInvalidDocument)
	}
	if rawSize := attribute(element, "size"); rawSize != "" {
		if size, err := strconv.ParseInt(rawSize, 10, 64); err != nil || size < 0 {
			return fmt.Errorf("%w: rom size", ErrInvalidDocument)
		}
	}
	crc := attribute(element, "crc")
	sha1Value := attribute(element, "sha1")
	if crc != "" && !validHex(crc, 8) || sha1Value != "" && !validHex(sha1Value, 40) {
		return fmt.Errorf("%w: rom hash", ErrInvalidDocument)
	}
	status := normalizedStatus(attribute(element, "status"))
	stats.ROMEntryCount++
	if attribute(element, "merge") != "" {
		stats.ROMEntryWithMergeCount++
	}
	if attribute(element, "bios") != "" {
		stats.ROMEntryWithBIOSCount++
	}
	if status == "nodump" {
		stats.ROMNodumpCount++
	}
	if status == "baddump" {
		stats.ROMBaddumpCount++
	}
	if crc == "" {
		stats.ROMMissingCRC32Count++
	}
	if sha1Value == "" {
		stats.ROMMissingSHA1Count++
	}
	if crc == "" && sha1Value == "" {
		stats.ROMMissingAllHashCount++
		if status != "nodump" {
			stats.NonNodumpROMMissingAllHashCount++
			return fmt.Errorf("%w: rom missing hash", ErrInvalidDocument)
		}
	}
	return nil
}

func validName(value string) bool {
	return value != "" && len(value) <= maxNameBytes && utf8.ValidString(value)
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		isDigit := character >= '0' && character <= '9'
		isLower := character >= 'a' && character <= 'f'
		isUpper := character >= 'A' && character <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}

func normalizedStatus(value string) string {
	if value == "" {
		return "good"
	}
	return strings.ToLower(value)
}

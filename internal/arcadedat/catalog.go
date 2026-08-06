package arcadedat

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Catalog struct {
	Stats    Stats
	Machines []Machine
}

type Machine struct {
	Name           string
	Description    string
	Year           string
	Manufacturer   string
	CloneOf        string
	ROMOf          string
	ExplicitBIOS   bool
	Classification string
	BIOSSets       []BIOSSet
	ROMs           []ROMEntry
	Disks          []DiskEntry
}

type BIOSSet struct {
	Name        string
	Description string
	Default     bool
}

type ROMEntry struct {
	Ordinal   int
	Name      string
	SizeBytes int64
	CRC32     string
	SHA1      string
	Status    string
	MergeName string
	BIOSName  string
}

type DiskEntry struct {
	Ordinal int
	Name    string
	SHA1    string
	Status  string
}

// ParseCatalog uses the same bounded, non-resolving validator as Parse before
// materializing the index rows consumed by matching and dependency planning.
//
//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func ParseCatalog(ctx context.Context, source io.Reader, coreID string) (Catalog, error) {
	contents, err := io.ReadAll(io.LimitReader(source, maxDATBytes+1))
	if err != nil {
		return Catalog{}, fmt.Errorf("arcadedat/catalog: %w", err)
	}
	if len(contents) > maxDATBytes {
		return Catalog{}, ErrLimitExceeded
	}
	stats, err := Parse(ctx, bytes.NewReader(contents), coreID)
	if err != nil {
		return Catalog{}, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	decoder.Strict = true
	catalog := Catalog{Stats: stats, Machines: make([]Machine, 0, stats.MachineCount)}
	var current *Machine
	var textField string
	var text strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return Catalog{}, fmt.Errorf("arcadedat/catalog: %w", err)
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Catalog{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "machine", "game":
				catalog.Machines = append(catalog.Machines, Machine{
					Name: attribute(value, "name"), CloneOf: attribute(value, "cloneof"), ROMOf: attribute(value, "romof"),
					ExplicitBIOS: attribute(value, "isbios") == "yes", Classification: "NORMAL",
				})
				current = &catalog.Machines[len(catalog.Machines)-1]
			case "description", "year", "manufacturer":
				if current != nil {
					textField = value.Name.Local
					text.Reset()
				}
			case "biosset":
				if current != nil {
					current.BIOSSets = append(current.BIOSSets, BIOSSet{
						Name:        attribute(value, "name"),
						Description: attribute(value, "description"),
						Default:     attribute(value, "default") == "yes",
					})
				}
			case "rom":
				if current != nil {
					size, _ := strconv.ParseInt(zeroIfEmpty(attribute(value, "size")), 10, 64)
					current.ROMs = append(current.ROMs, ROMEntry{
						Ordinal:   len(current.ROMs),
						Name:      attribute(value, "name"),
						SizeBytes: size,
						CRC32:     strings.ToLower(attribute(value, "crc")),
						SHA1:      strings.ToLower(attribute(value, "sha1")),
						Status:    catalogStatus(attribute(value, "status")),
						MergeName: attribute(value, "merge"),
						BIOSName:  attribute(value, "bios"),
					})
				}
			case "disk":
				if current != nil {
					current.Disks = append(current.Disks, DiskEntry{
						Ordinal: len(current.Disks),
						Name:    attribute(value, "name"),
						SHA1:    strings.ToLower(attribute(value, "sha1")),
						Status:  catalogStatus(attribute(value, "status")),
					})
				}
			}
		case xml.CharData:
			if textField != "" {
				_, _ = text.Write(value)
			}
		case xml.EndElement:
			if textField != "" && value.Name.Local == textField && current != nil {
				field := strings.TrimSpace(text.String())
				if !utf8.ValidString(field) || len(field) > maxFieldBytes {
					return Catalog{}, ErrLimitExceeded
				}
				switch textField {
				case "description":
					current.Description = field
				case "year":
					current.Year = field
				case "manufacturer":
					current.Manufacturer = field
				}
				textField = ""
			}
			if (value.Name.Local == "machine" || value.Name.Local == "game") && current != nil {
				if err := validateMachine(*current); err != nil {
					return Catalog{}, err
				}
				current = nil
			}
		}
	}
	classifications := make(map[string]string)
	for _, machine := range catalog.Machines {
		if machine.ExplicitBIOS {
			classifications[machine.Name] = "EXPLICIT_BIOS"
		}
		if machine.ROMOf != "" && machine.ROMOf != machine.CloneOf {
			if _, exists := classifications[machine.ROMOf]; !exists {
				classifications[machine.ROMOf] = "ROMOF_INFERENCE"
			}
		}
	}
	for index := range catalog.Machines {
		if classification := classifications[catalog.Machines[index].Name]; classification != "" {
			catalog.Machines[index].Classification = classification
		}
	}
	return catalog, nil
}

func validateMachine(machine Machine) error {
	biosNames := make(map[string]struct{}, len(machine.BIOSSets))
	defaults := 0
	for _, bios := range machine.BIOSSets {
		if !validName(bios.Name) {
			return fmt.Errorf("%w: bios name", ErrInvalidDocument)
		}
		if _, duplicate := biosNames[bios.Name]; duplicate {
			return fmt.Errorf("%w: duplicate bios", ErrInvalidDocument)
		}
		biosNames[bios.Name] = struct{}{}
		if bios.Default {
			defaults++
		}
	}
	if len(machine.BIOSSets) > 0 && defaults != 1 {
		return fmt.Errorf("%w: default bios", ErrInvalidDocument)
	}
	for _, rom := range machine.ROMs {
		if rom.BIOSName != "" {
			if _, exists := biosNames[rom.BIOSName]; !exists {
				return fmt.Errorf("%w: unknown rom bios", ErrInvalidDocument)
			}
		}
	}
	return nil
}

func zeroIfEmpty(value string) string {
	if value == "" {
		return "0"
	}
	return value
}

func catalogStatus(value string) string {
	switch normalizedStatus(value) {
	case "nodump":
		return "NODUMP"
	case "baddump":
		return "BADDUMP"
	default:
		return "GOOD"
	}
}

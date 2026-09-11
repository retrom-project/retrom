package firmwaremanifest

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
)

var ErrInvalid = errors.New("invalid firmware catalog")

//go:embed catalog.json
var catalogJSON []byte

type Member struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	CRC32     string `json:"crc32"`
	SHA1      string `json:"sha1"`
	Required  bool   `json:"required"`
}

type Item struct {
	LogicalName  string   `json:"logicalName"`
	EmulatorPath string   `json:"emulatorPath"`
	Mode         string   `json:"mode"`
	Members      []Member `json:"members"`
}

type Catalog struct {
	SchemaVersion int    `json:"schemaVersion"`
	ParserVersion string `json:"parserVersion"`
	Source        struct {
		CoreID       string `json:"coreId"`
		ProviderID   string `json:"providerId"`
		TargetID     string `json:"targetId"`
		SourceURL    string `json:"sourceUrl"`
		SourceSHA256 string `json:"sourceSha256"`
	} `json:"source"`
	Items []Item `json:"items"`
}

func Load() (Catalog, error) {
	var result Catalog
	if err := json.Unmarshal(catalogJSON, &result); err != nil {
		return result, fmt.Errorf("decode firmware catalog: %w", err)
	}
	if result.SchemaVersion != 1 || result.ParserVersion == "" ||
		!validHex(result.Source.SourceSHA256, 64) || len(result.Items) == 0 {
		return result, fmt.Errorf("%w: provenance", ErrInvalid)
	}
	for _, item := range result.Items {
		if item.Mode != "REQUIRED" && item.Mode != "OPTIONAL" || path.Base(item.EmulatorPath) != item.LogicalName {
			return result, fmt.Errorf("%w: item", ErrInvalid)
		}
		if err := ValidateMembers(item.Members); err != nil {
			return result, err
		}
	}
	return result, nil
}

func DecodeMembers(value string) ([]Member, error) {
	var members []Member
	if err := json.Unmarshal([]byte(value), &members); err != nil {
		return nil, fmt.Errorf("decode firmware members: %w", err)
	}
	if err := ValidateMembers(members); err != nil {
		return nil, err
	}
	return members, nil
}

func ValidateMembers(members []Member) error {
	seen := make(map[string]bool)
	required := false
	for _, member := range members {
		name := strings.ToLower(member.Name)
		if seen[name] || !validMemberPath(member.Name) || member.SizeBytes < 1 ||
			!validHex(member.CRC32, 8) || !validHex(member.SHA1, 40) {
			return fmt.Errorf("%w: member", ErrInvalid)
		}
		seen[name] = true
		required = required || member.Required
	}
	if !required {
		return fmt.Errorf("%w: no required members", ErrInvalid)
	}
	return nil
}

func validHex(value string, length int) bool {
	_, err := hex.DecodeString(value)
	return len(value) == length && err == nil
}

func validMemberPath(name string) bool {
	return name != "" && name != "." && name != ".." && path.Clean(name) == name &&
		!strings.ContainsAny(name, "\x00\r\n\\") && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "../")
}

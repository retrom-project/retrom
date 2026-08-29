package packs

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

//go:embed easy-rtp-layout-v1.json
var easyRTPLayoutBytes []byte

type easyRTPLayout struct {
	SchemaVersion      int    `json:"schemaVersion"`
	SourcePlayerCommit string `json:"sourcePlayerCommit"`
	Extensions         struct {
		Image []string `json:"image"`
		Movie []string `json:"movie"`
		Music []string `json:"music"`
		Sound []string `json:"sound"`
	} `json:"extensions"`
	Generations map[string]struct {
		Categories          []string `json:"categories"`
		RegisteredResources []string `json:"registeredResources"`
	} `json:"generations"`
}

type layoutViolation struct {
	Path   string
	Reason string
}

func (violation *layoutViolation) Error() string {
	return violation.Path + ": " + violation.Reason
}

func (violation *layoutViolation) Unwrap() error {
	return ErrInvalid
}

var (
	layoutOnce sync.Once
	layoutData easyRTPLayout
	layoutErr  error
)

const easyRTPPlayerCommit = "78328fa29f465315291e161130e6682f69410370"

func loadEasyRTPLayout() (easyRTPLayout, error) {
	layoutOnce.Do(func() {
		layoutErr = json.Unmarshal(easyRTPLayoutBytes, &layoutData)
		if layoutErr == nil && (layoutData.SchemaVersion != 1 ||
			layoutData.SourcePlayerCommit != easyRTPPlayerCommit) {
			layoutErr = errLayoutData
		}
	})
	return layoutData, layoutErr
}

// ValidateEasyRTPLayout verifies that every file belongs to a category and
// decoder extension fixed by the pinned Player source, and that at least one
// filename is present in its pinned RTP alias table.
func ValidateEasyRTPLayout(generation string, files []FileIdentity) error {
	layout, err := loadEasyRTPLayout()
	if err != nil {
		return fmt.Errorf("%w: load EasyRPG layout: %w", ErrInvalid, err)
	}
	profile, found := layout.Generations[generation]
	if !found || len(files) == 0 {
		return ErrInvalid
	}
	categories := foldedSet(profile.Categories)
	registered := foldedSet(profile.RegisteredResources)
	foundRegistered := false
	for _, file := range files {
		category, name, valid := strings.Cut(file.Path, "/")
		if !valid || category == "" || name == "" || strings.Contains(name, "/") {
			return &layoutViolation{
				Path: file.Path, Reason: "必须是已登记分类下的单层资源文件",
			}
		}
		foldedCategory := foldLayoutKey(category)
		if _, exists := categories[foldedCategory]; !exists {
			return &layoutViolation{Path: file.Path, Reason: "不属于固定 EasyRPG RTP 分类"}
		}
		if !validRTPResourceExtension(layout, foldedCategory, name) {
			return &layoutViolation{Path: file.Path, Reason: "扩展名不在固定 Player 可解码集合中"}
		}
		stem := strings.TrimSuffix(name, path.Ext(name))
		if _, exists := registered[foldLayoutKey(category+"/"+stem)]; exists {
			foundRegistered = true
		}
	}
	if !foundRegistered {
		return ErrInvalid
	}
	return nil
}

func validRTPResourceExtension(layout easyRTPLayout, category, name string) bool {
	extensions := layout.Extensions.Image
	switch category {
	case "movie":
		extensions = layout.Extensions.Movie
	case "music":
		extensions = layout.Extensions.Music
	case "sound":
		extensions = layout.Extensions.Sound
	}
	extension := strings.ToLower(path.Ext(name))
	for _, allowed := range extensions {
		if extension == allowed {
			return true
		}
	}
	return false
}

func foldedSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[foldLayoutKey(value)] = struct{}{}
	}
	return result
}

func foldLayoutKey(value string) string {
	return cases.Fold().String(norm.NFKC.String(value))
}

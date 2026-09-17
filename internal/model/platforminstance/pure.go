package platforminstance

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/runtime/platformcatalog"
)

// SlugBase produces a URL-safe base slug from a display name and platform ID.
func SlugBase(name, platformID string) string {
	toSlug := func(value string) string {
		var builder strings.Builder
		separator := false
		for _, character := range value {
			if character >= 'A' && character <= 'Z' {
				character += 'a' - 'A'
			}
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
				if separator && builder.Len() > 0 && builder.Len() < 80 {
					builder.WriteByte('-')
				}
				separator = false
				if builder.Len() < 80 {
					builder.WriteRune(character)
				}
				continue
			}
			separator = builder.Len() > 0
		}
		return strings.TrimRight(builder.String(), "-")
	}
	if slug := toSlug(name); slug != "" {
		return slug
	}
	prefix := toSlug(platformID)
	if prefix == "" {
		prefix = "game"
	}
	return prefix + "-library"
}

// SlugWithSuffix appends a numeric suffix to a slug base.
func SlugWithSuffix(base string, suffix int) string {
	if suffix < 2 {
		return base
	}
	ending := "-" + strconv.Itoa(suffix)
	prefix := strings.TrimRight(base[:min(len(base), 80-len(ending))], "-")
	return prefix + ending
}

// NextSlug selects an unused slug from the repository's reserved names.
func NextSlug(base string, slugs []string) (string, error) {
	used := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		used[slug] = struct{}{}
	}
	for suffix := 1; suffix <= len(used)+1; suffix++ {
		candidate := SlugWithSuffix(base, suffix)
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
	}
	return "", ErrSlugExhausted
}

// ImpactDigest computes a deterministic digest for a CoreImpact value.
func ImpactDigest(value CoreImpact) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// ProjectCoreImpact computes the impact result from facts.
func ProjectCoreImpact(instanceID, coreID string, facts CoreImpactFacts) CoreImpactResult {
	counts := map[string]int64{"ready": 0, "needsValidation": 0, "blocked": 0}
	items := make([]CoreImpactItem, 0, len(facts.Games))
	for _, game := range facts.Games {
		status := "NEEDS_VALIDATION"
		switch {
		case game.VariantStatus != nil && *game.VariantStatus == "READY":
			status = "READY"
			counts["ready"]++
		case game.VariantStatus != nil:
			status = "BLOCKED"
			counts["blocked"]++
		default:
			counts["needsValidation"]++
		}
		items = append(items, CoreImpactItem{GameID: game.GameID, Status: status, BlockerCode: game.TargetCompatibilityCode})
	}
	return CoreImpactResult{
		Impact: CoreImpact{
			Action: "CHANGE_DEFAULT_CORE", PlatformInstanceID: instanceID,
			PlatformInstanceVersion: facts.PlatformInstanceVersion, CoreID: coreID,
			ProviderID: facts.ProviderID, TargetID: facts.TargetID, BundleSHA256: facts.BundleSHA256,
			DATVersionID: facts.DATVersionID, Games: facts.Games,
		},
		Counts: counts,
		Items:  items,
	}
}

type directoryIndex struct {
	activeByKey     map[string]Directory
	activeByPair    map[string]Directory
	suppressedByKey map[string]Directory
}

func indexDirectoryRows(rows []Directory) directoryIndex {
	index := directoryIndex{
		activeByKey:     make(map[string]Directory),
		activeByPair:    make(map[string]Directory),
		suppressedByKey: make(map[string]Directory),
	}
	for _, row := range rows {
		if row.Deleted || !row.Enabled {
			addCatalogRow(index.suppressedByKey, row)
			continue
		}
		addCatalogRow(index.activeByKey, row)
		addDirectoryRow(index.activeByPair, row.PlatformID+"/"+row.CoreID, row)
	}
	return index
}

func addCatalogRow(target map[string]Directory, row Directory) {
	if row.CatalogKey != nil {
		addDirectoryRow(target, *row.CatalogKey, row)
	}
}

func addDirectoryRow(target map[string]Directory, key string, row Directory) {
	if _, exists := target[key]; !exists {
		target[key] = row
	}
}

// ProjectRecommendations produces recommendations from catalog and directory data.
func ProjectRecommendations(
	catalog platformcatalog.Catalog,
	references map[string]CatalogReference,
	rows []Directory,
) Recommendations {
	index := indexDirectoryRows(rows)
	result := Recommendations{
		CatalogVersion: catalog.Version,
		Items:          make([]Recommendation, 0, len(catalog.Templates)),
	}
	result.Summary.TotalCount = len(catalog.Templates)
	for _, template := range catalog.Templates {
		reference := references[template.Key]
		item := Recommendation{
			TemplateKey: template.Key, CatalogOrder: template.CatalogOrder,
			Name: template.Name, Description: template.Description,
			Platform:            Reference{ID: template.PlatformID, Name: reference.PlatformName},
			DefaultCore:         Reference{ID: template.DefaultCoreID, Name: reference.CoreName},
			SupportedExtensions: contentprofile.SupportedExtensions(template.PlatformID),
		}
		projectRecommendationState(&item, &result.Summary, template, index)
		result.Items = append(result.Items, item)
	}
	return result
}

func projectRecommendationState(
	item *Recommendation,
	summary *RecommendationSummary,
	template platformcatalog.DirectoryTemplate,
	index directoryIndex,
) {
	if row, exists := index.activeByKey[template.Key]; exists {
		item.PlatformInstanceID = stringPointer(row.ID)
		if directoryMatchesTemplate(row, template) {
			item.State = StateActive
			summary.ActiveCount++
			return
		}
		item.State = StateCustomized
		summary.CustomizedCount++
		return
	}
	if row, exists := index.activeByPair[template.Key]; exists {
		item.State = StateCoveredByEquivalent
		item.PlatformInstanceID = stringPointer(row.ID)
		summary.CoveredByEquivalentCount++
		return
	}
	if row, exists := index.suppressedByKey[template.Key]; exists {
		item.State = StateSuppressed
		item.PlatformInstanceID = stringPointer(row.ID)
		summary.SuppressedCount++
		return
	}
	item.State = StateMissing
	summary.MissingCount++
}

func directoryMatchesTemplate(row Directory, template platformcatalog.DirectoryTemplate) bool {
	return row.PlatformID == template.PlatformID && row.CoreID == template.DefaultCoreID &&
		row.Name == template.Name && row.Description == template.Description
}

// CatalogTemplate finds a template by key.
func CatalogTemplate(catalog platformcatalog.Catalog, key string) platformcatalog.DirectoryTemplate {
	for _, template := range catalog.Templates {
		if template.Key == key {
			return template
		}
	}
	return platformcatalog.DirectoryTemplate{}
}

func stringPointer(value string) *string {
	return &value
}

package platforminstance

import (
	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/runtime/platformcatalog"
)

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

func projectRecommendations(
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

func catalogTemplate(catalog platformcatalog.Catalog, key string) platformcatalog.DirectoryTemplate {
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

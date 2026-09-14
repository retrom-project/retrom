package emulationstationimport

import "encoding/json"

func (result *ScanProjection) CollectItem(item ScanItem) {
	result.Items = append(result.Items, item)
	var warnings []struct {
		PathKind string `json:"pathKind"`
	}
	if json.Unmarshal([]byte(item.WarningsJSON), &warnings) == nil {
		for _, warning := range warnings {
			if warning.PathKind == "COVER" || warning.PathKind == "VIDEO" {
				result.MediaWarnings++
			}
		}
	}
	for _, file := range item.Files {
		result.AddEstimated(file.Size)
	}
	for _, asset := range item.Assets {
		if asset.State != "DISCOVERED" || asset.Size == nil {
			continue
		}
		result.AddEstimated(*asset.Size)
		if asset.Kind == "COVER" {
			result.Covers++
		} else {
			result.Videos++
		}
	}
}

func (result *ScanProjection) AddEstimated(value int64) {
	const maximum = int64(2 << 40)
	if value < 0 || result.EstimatedBytes > maximum-value {
		result.EstimatedBytes = maximum + 1
		return
	}
	result.EstimatedBytes += value
}

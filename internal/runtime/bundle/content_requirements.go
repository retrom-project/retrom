package runtimebundle

import (
	"slices"

	"retrom/internal/content/requirements"
)

type ArcadeDAT struct {
	Format     string             `json:"format"`
	Asset      requirements.Asset `json:"asset"`
	Core       requirements.Asset `json:"core"`
	Provenance requirements.Asset `json:"provenance"`
}

func (value ArcadeDAT) Valid() bool {
	return value.Format == "ARCADE_XML" && value.Asset.Valid() && value.Core.Valid() && value.Provenance.Valid()
}

func validRequirementShape(raw any) bool {
	rule, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	switch rule["kind"] {
	case requirements.Decrypted3DS:
		return exactMap(rule, "kind")
	case requirements.FlycastCartridge:
		return exactMap(rule, "kind", "platform", "catalog", "core") &&
			validAssetShape(rule["catalog"]) && validAssetShape(rule["core"])
	default:
		return false
	}
}

func validAssetShape(raw any) bool {
	value, ok := raw.(map[string]any)
	return ok && exactMap(value, "path", "sha256")
}

func validDATShape(raw any) bool {
	value, ok := raw.(map[string]any)
	return ok && exactMap(value, "format", "asset", "core", "provenance") &&
		validAssetShape(value["asset"]) && validAssetShape(value["core"]) && validAssetShape(value["provenance"])
}

func targetRequirementAssets(target Target) []requirements.Asset {
	var assets []requirements.Asset
	if rule := target.ContentRequirements; rule != nil && rule.Kind == requirements.FlycastCartridge {
		assets = append(assets, *rule.Catalog, *rule.Core)
	}
	if dat := target.ArcadeDAT; dat != nil {
		assets = append(assets, dat.Asset, dat.Core, dat.Provenance)
	}
	return assets
}

func validRequirementAssets(target Target) bool {
	if target.ArcadeDAT != nil && !target.ArcadeDAT.Valid() {
		return false
	}
	for _, asset := range targetRequirementAssets(target) {
		if !slices.Contains(target.AssetPaths, asset.Path) {
			return false
		}
	}
	return true
}

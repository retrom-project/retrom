package runtimeprovider

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"regexp"

	"retrom/internal/content/requirements"
	runtimebundle "retrom/internal/runtime/bundle"
)

type (
	VerifiedDAT struct{ ProviderID, TargetID, Path, SHA256, SourceCommit string }
	datPair     struct {
		SchemaVersion int    `json:"schemaVersion"`
		Kind          string `json:"kind"`
		Core          struct {
			Filename string `json:"filename"`
			SHA256   string `json:"sha256"`
		} `json:"core"`
		DAT struct {
			Filename string `json:"filename"`
			SHA256   string `json:"sha256"`
		} `json:"dat"`
		SourceCommit string `json:"sourceCommit"`
		Generator    struct {
			Method     string `json:"method"`
			Symbol     string `json:"symbol"`
			WasmSHA256 string `json:"wasmSha256"`
			SHA256     string `json:"sha256"`
		} `json:"generator"`
		BuildConfigSHA256 string `json:"buildConfigSha256"`
	}
)

var (
	sourceCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
	assetDigestPattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func loadContentRequirements(root string, active runtimebundle.ActiveDescriptor,
	manifests map[string]runtimebundle.Manifest, integrity map[string][]runtimebundle.IntegrityFile,
) ([]VerifiedDAT, error) {
	var dats []VerifiedDAT
	for _, provider := range active.Providers {
		manifest := manifests[provider.ProviderID]
		loader := contentRequirementLoader{
			directory: filepath.Join(root, filepath.FromSlash(provider.InstallationPath)),
			files:     map[string]runtimebundle.IntegrityFile{}, verified: map[string]bool{},
		}
		for _, file := range integrity[provider.ProviderID] {
			loader.files[file.Path] = file
		}
		for index := range manifest.Targets {
			target := &manifest.Targets[index]
			if err := loader.loadCatalog(target.ContentRequirements); err != nil {
				return nil, err
			}
			if target.ArcadeDAT == nil {
				continue
			}
			dat, err := loader.loadDAT(*target.ArcadeDAT)
			if err != nil {
				return nil, err
			}
			dat.ProviderID, dat.TargetID = provider.ProviderID, target.ID
			dats = append(dats, dat)
		}
		manifests[provider.ProviderID] = manifest
	}
	return dats, nil
}

type contentRequirementLoader struct {
	directory string
	files     map[string]runtimebundle.IntegrityFile
	verified  map[string]bool
}

func (loader *contentRequirementLoader) verify(asset requirements.Asset) error {
	expected, found := loader.files[asset.Path]
	if !found || expected.SHA256 != asset.SHA256 {
		return ErrInstallationInvalid
	}
	if loader.verified[asset.Path] {
		return nil
	}
	if err := verifyInstalledFile(filepath.Join(loader.directory, filepath.FromSlash(asset.Path)), expected); err != nil {
		return err
	}
	loader.verified[asset.Path] = true
	return nil
}

func (loader *contentRequirementLoader) loadCatalog(policy *requirements.Policy) error {
	if policy == nil || policy.Kind != requirements.FlycastCartridge {
		return nil
	}
	if loader.verify(*policy.Core) != nil || loader.verify(*policy.Catalog) != nil {
		return ErrInstallationInvalid
	}
	contents, err := readMetadata(filepath.Join(loader.directory, filepath.FromSlash(policy.Catalog.Path)))
	if err != nil {
		return err
	}
	catalog, err := requirements.ParseFlycastCatalog(contents, policy)
	if err != nil {
		return installationInvalid(err)
	}
	policy.CatalogFacts, policy.CatalogJSON = catalog, contents
	return nil
}

func (loader *contentRequirementLoader) loadDAT(dat runtimebundle.ArcadeDAT) (VerifiedDAT, error) {
	if loader.verify(dat.Core) != nil || loader.verify(dat.Asset) != nil || loader.verify(dat.Provenance) != nil {
		return VerifiedDAT{}, ErrInstallationInvalid
	}
	contents, err := readMetadata(filepath.Join(loader.directory, filepath.FromSlash(dat.Provenance.Path)))
	if err != nil || !validDATPair(contents, dat) {
		return VerifiedDAT{}, ErrInstallationInvalid
	}
	var pair datPair
	if json.Unmarshal(contents, &pair) != nil {
		return VerifiedDAT{}, ErrInstallationInvalid
	}
	return VerifiedDAT{
		SourceCommit: pair.SourceCommit,
		Path:         filepath.Join(loader.directory, filepath.FromSlash(dat.Asset.Path)), SHA256: dat.Asset.SHA256,
	}, nil
}

func validDATPair(contents []byte, expected runtimebundle.ArcadeDAT) bool {
	var pair datPair
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pair) != nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	return pair.SchemaVersion == 1 && pair.Kind == "CORE_DAT_PAIR" &&
		pair.Core.Filename == filepath.Base(expected.Core.Path) && pair.Core.SHA256 == expected.Core.SHA256 &&
		pair.DAT.Filename == filepath.Base(expected.Asset.Path) && pair.DAT.SHA256 == expected.Asset.SHA256 &&
		validPairProvenance(pair)
}

func validPairProvenance(pair datPair) bool {
	return sourceCommitPattern.MatchString(pair.SourceCommit) && pair.Generator.Method == "SHIPPED_WASM" &&
		pair.Generator.Symbol != "" && assetDigestPattern.MatchString(pair.Generator.WasmSHA256) &&
		assetDigestPattern.MatchString(pair.Generator.SHA256) && assetDigestPattern.MatchString(pair.BuildConfigSHA256)
}

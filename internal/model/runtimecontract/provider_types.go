package runtimecontract

type Manifest struct {
	SchemaVersion    int
	ProviderID       string
	ProviderVersion  string
	ProviderAPI      int
	ClientModulePath string
	Targets          []Target
}

type Target struct {
	ID                  string              `json:"id"`
	DisplayName         string              `json:"displayName"`
	TargetOptionsSchema TargetOptionsSchema `json:"targetOptionsSchema"`
	Inputs              []Input             `json:"inputs"`
	Capabilities        Capabilities        `json:"capabilities"`
	Checkpoint          *Checkpoint         `json:"checkpoint"`
	AssetPaths          []string            `json:"assetPaths"`
}

type Input struct {
	Role        string `json:"role"`
	Kind        string `json:"kind"`
	Cardinality string `json:"cardinality"`
	Optional    bool   `json:"optional"`
}

type Capabilities struct {
	Pause           bool     `json:"pause"`
	Screenshot      bool     `json:"screenshot"`
	Checkpoint      bool     `json:"checkpoint"`
	StandardGamepad bool     `json:"standardGamepad"`
	FrameCounter    bool     `json:"frameCounter"`
	Volume          bool     `json:"volume"`
	DiscSwitch      bool     `json:"discSwitch"`
	NativeSettings  bool     `json:"nativeSettings"`
	InputFilter     bool     `json:"inputFilter"`
	NetplayPort     bool     `json:"netplayPort"`
	VideoModes      []string `json:"videoModes"`
	RequiresThreads bool     `json:"requiresThreads"`
	FrameMode       string   `json:"frameMode"`
}

type Checkpoint struct {
	WriteFormat string   `json:"writeFormat"`
	ReadFormats []string `json:"readFormats"`
	MaxBytes    int64    `json:"maxBytes"`
	Semantics   string   `json:"semantics,omitempty"`
}

type IntegrityFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"-"`
}

type Integrity struct {
	SchemaVersion int
	Files         []IntegrityFile
}
type ActiveDescriptor struct {
	SchemaVersion    int              `json:"schemaVersion"`
	Source           string           `json:"source"`
	SourceTreeSHA256 *string          `json:"sourceTreeSha256"`
	Release          *ReleaseIdentity `json:"release"`
	Providers        []ActiveProvider `json:"providers"`
}

type ReleaseIdentity struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Commit     string `json:"commit"`
}

type ActiveProvider struct {
	ProviderID        string         `json:"providerId"`
	ProviderVersion   string         `json:"providerVersion"`
	ProviderAPI       int            `json:"providerApiVersion"`
	BundleSHA256      string         `json:"bundleSha256"`
	BundleSizeBytes   int64          `json:"bundleSizeBytes"`
	ManifestSHA256    string         `json:"manifestSha256"`
	ModuleSHA256      string         `json:"moduleSha256"`
	ClientModulePath  string         `json:"clientModulePath"`
	InstallationPath  string         `json:"installationPath"`
	FileCount         int64          `json:"fileCount"`
	UnpackedSizeBytes int64          `json:"unpackedSizeBytes"`
	Targets           []ActiveTarget `json:"targets"`
}

type ActiveTarget struct {
	ID         string      `json:"id"`
	Checkpoint *Checkpoint `json:"checkpoint"`
}

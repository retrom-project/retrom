package model

// RuntimeProviderModule identifies verified browser code without creating a Run.
type RuntimeProviderModule struct {
	ProviderID         string `json:"providerId"`
	ProviderVersion    string `json:"providerVersion"`
	ProviderAPIVersion int    `json:"providerApiVersion"`
	BundleSHA256       string `json:"bundleSha256"`
	ModuleSHA256       string `json:"moduleSha256"`
	ModuleURL          string `json:"moduleUrl"`
	RuntimeBaseURL     string `json:"runtimeBaseUrl"`
}

package metadatascrape

type MediaInput struct {
	AssetID      string `json:"candidateAssetId"`
	RunID        string `json:"scrapeRunId"`
	ResponseID   string `json:"providerResponseId"`
	SourceDigest string `json:"sourceDigest"`
}

type MediaInputScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type MediaInputEnvelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Kind          string          `json:"kind"`
	Scope         MediaInputScope `json:"scope"`
	ExecutionID   string          `json:"executionId"`
	Inputs        MediaInput      `json:"inputs"`
}

type MediaJobPlan struct {
	JobID, RunID, AssetID, InputJSON, InputDigest, Dedupe string
	Scope                                                 Subject
	Now                                                   int64
}

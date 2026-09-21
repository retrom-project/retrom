package launch

type Capabilities struct {
	SecureContext       bool `json:"secureContext"`
	CrossOriginIsolated bool `json:"crossOriginIsolated"`
	SharedArrayBuffer   bool `json:"sharedArrayBuffer"`
}

type Created struct {
	Status               string   `json:"status,omitempty"`
	JobID                string   `json:"jobId,omitempty"`
	RetryAfterMS         int64    `json:"retryAfterMs,omitempty"`
	LaunchID             string   `json:"launchId"`
	PlayURL              string   `json:"playUrl"`
	Warnings             []string `json:"warnings"`
	BootstrapExpiresAtMS int64    `json:"bootstrapExpiresAtMs"`
	HardExpiresAtMS      int64    `json:"hardExpiresAtMs"`
	Capability           string   `json:"-"`
	Existing             bool     `json:"-"`
}

type NetplayCreateRequest struct {
	RoomID                  string
	SessionID               string
	ProfileID               string
	PlayerNo                int
	GameID                  string
	GameVariantID           string
	ProviderID              string
	TargetID                string
	BundleSHA256            string
	ReturnTo                string
	ClientCapabilities      Capabilities
	CredentialGeneration    int64
	NetplayCredentialSHA256 []byte
}

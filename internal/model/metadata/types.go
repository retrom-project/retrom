package metadata

import "encoding/json"

type ContentHashes struct {
	MD5    string
	SHA1   string
	SHA256 string
	CRC32  string
}

type ProviderOutcome string

const (
	OutcomeHit             ProviderOutcome = "HIT"
	OutcomeMiss            ProviderOutcome = "MISS"
	OutcomeRateLimited     ProviderOutcome = "RATE_LIMITED"
	OutcomeTimeout         ProviderOutcome = "TIMEOUT"
	OutcomeInvalidResponse ProviderOutcome = "INVALID_RESPONSE"
	OutcomeNetworkError    ProviderOutcome = "NETWORK_ERROR"
)

type Candidate struct {
	ProviderGameID string           `json:"providerGameId"`
	Metadata       json.RawMessage  `json:"metadata"`
	Evidence       json.RawMessage  `json:"evidence"`
	Assets         []AssetReference `json:"assets"`
}

type AssetReference struct {
	ProviderAssetID string `json:"providerAssetId"`
	Kind            string `json:"kind"`
	Ordinal         int    `json:"ordinal"`
	Path            string `json:"path"`
}

type LookupResult struct {
	Outcome       ProviderOutcome
	Audit         ProtocolAudit
	Candidate     *Candidate
	RawResponse   []byte
	RetryAfterNS  int64
	RequestBody   []byte
	RequestDigest string
}

type AssetData struct {
	ReceivedBytes int64
	Bytes         []byte
	MediaType     string
	Width         int
	Height        int
}

// ProtocolAudit contains opaque provider protocol evidence. Only the acquiring
// Adapter and the persistence boundary encode or interpret its audit fields;
// Service and Model use ProviderOutcome for business decisions.
type ProtocolAudit []byte

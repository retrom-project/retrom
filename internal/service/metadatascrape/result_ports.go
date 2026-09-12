package metadatascrape

import (
	"context"

	"retrom/internal/blobstore"
	"retrom/internal/hasheous"
)

type LookupAttempt struct {
	Claim          WorkerClaim
	EvidenceID     string
	Lookup         ResolvedLookup
	AttemptNo      int
	AllowCandidate bool
}
type ResponseRecord struct {
	ID, RequestDigest string
	Outcome           hasheous.ProviderOutcome
	HTTPStatus        int
	Blob              *blobstore.Metadata
	Cacheable         bool
	Now, ExpiresAt    int64
}
type AttemptRecord struct {
	ID, RunID, EvidenceID, ResponseID, Source string
	AttemptNo                                 int
	Now                                       int64
}
type CandidateRecord struct {
	ID, RunID, ResponseID, ProviderGameID, MetadataJSON, EvidenceJSON string
	Now                                                               int64
}
type CandidateIdentity struct {
	ID      string
	Created bool
}
type CandidateHit struct {
	CandidateID, AttemptID, HashesJSON string
	Now                                int64
}
type CandidateAsset struct {
	ID, CandidateID, ResponseID string
	Reference                   hasheous.AssetRef
	Now                         int64
}
type ResultReader interface {
	Writable(context.Context, WorkerClaim) (bool, error)
	Hashes(context.Context, string) (Hashes, error)
}
type ResultWriter interface {
	Response(context.Context, ResponseRecord) error
	Attempt(context.Context, AttemptRecord) error
	Candidate(context.Context, CandidateRecord) (CandidateIdentity, error)
	Hit(context.Context, CandidateHit) error
	Assets(context.Context, []CandidateAsset) error
}
type ResultScope struct {
	Read  ResultReader
	Write ResultWriter
}
type ResultRepository interface {
	WithWrite(context.Context, func(ResultScope) error) error
}

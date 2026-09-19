package libraryimport

import (
	"context"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/model/tagging"
)

type ImportRequest struct {
	UploadID                 string   `json:"uploadId"`
	TargetPlatformInstanceID string   `json:"targetPlatformInstanceId"`
	MetadataProvider         string   `json:"metadataProvider"`
	ContentMode              string   `json:"contentMode,omitempty"`
	TagIDs                   []string `json:"tagIds"`
}

type ImportFile struct {
	ID, Path, BlobID, SHA256 string
	Size                     int64
}
type ImportUpload struct {
	ID, Purpose, SourceType, State, ManifestDigest string
	Version, FileCount                             int64
}
type ImportTarget struct {
	ID, PlatformID, DefaultCoreID, CoreID, BindingID, ProviderID, TargetID, DeliveryProfile string
	Version                                                                                 int64
	Policy                                                                                  contentcapability.Policy
}
type ImportBinding struct {
	BindingID, CoreID, ProviderID, TargetID, DeliveryProfile, DetectorProfile string
	Policy                                                                    contentcapability.Policy
}
type ImportBindingQuery struct{ PlatformID, CoreID, DetectorProfile string }

type ImportTargetGuard struct {
	ProviderID string `json:"providerId"`
	TargetID   string `json:"targetId"`
	CoreID     string `json:"coreId"`
}
type ImportTargetSnapshot struct {
	SchemaVersion           int                 `json:"schemaVersion"`
	DefaultCoreID           string              `json:"defaultCoreId"`
	PlatformID              string              `json:"platformId"`
	PlatformInstanceID      string              `json:"platformInstanceId"`
	PlatformInstanceVersion int64               `json:"platformInstanceVersion"`
	Targets                 []ImportTargetGuard `json:"targets"`
}
type QueuedImportRequest struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Request       ImportRequest       `json:"request"`
	Tags          []tagging.Reference `json:"tags"`
}

type ImportAdmissionRepository interface {
	WithAdmission(context.Context, func(ImportAdmissionScope) error) error
}
type ImportAdmissionScope struct {
	Facts  ImportFactsReader
	Tags   tagging.WriteScope
	Writer ImportAdmissionWriter
}
type ImportFactsReader interface {
	Upload(context.Context, string) (ImportUpload, bool, error)
	Target(context.Context, string) (ImportTarget, bool, error)
	Files(context.Context, string) ([]ImportFile, error)
	Bindings(context.Context, ImportBindingQuery) ([]ImportBinding, error)
}
type ImportAdmissionWriter interface {
	Create(context.Context, ImportAdmissionChange) error
}
type ImportGroupNotifier interface{ NotifyImportGroup(context.Context, string) }

type ImportAdmissionChange struct {
	ImportID, JobID, ExecutionID, ConsumptionID string
	Request                                     ImportRequest
	Upload                                      ImportUpload
	Target                                      ImportTarget
	TargetSnapshot                              ImportTargetSnapshot
	Files                                       []ImportFile
	ActorUserID                                 string
	NowMS                                       int64
	ContentMode                                 string
	Documents                                   ImportAdmissionDocuments
}
type ImportAdmissionDocuments struct {
	RequestJSON, RequestDigest, TargetJSON, TargetDigest        string
	ConfigJSON, ConfigDigest, InputJSON, InputDigest, DedupeKey string
}

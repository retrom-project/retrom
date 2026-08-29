package checkpoint

import "errors"

const (
	Magic            = "RTRPGSV1"
	MaxManifestBytes = 256 << 10
	MaxEntries       = 512
	MaxBundleBytes   = 64 << 20
)

var ErrInvalid = errors.New("RPG_CHECKPOINT_INVALID")

type Engine string

const (
	EngineRPG2000 Engine = "RPG2000"
	EngineRPG2003 Engine = "RPG2003"
	EngineRPGMV   Engine = "RPGMV"
	EngineRPGMZ   Engine = "RPGMZ"
)

type Store string

const (
	StoreFilesystem   Store = "FILESYSTEM"
	StoreLocalStorage Store = "LOCAL_STORAGE"
	StoreLocalForage  Store = "LOCALFORAGE"
	StoreRetromNative Store = "RETROM_NATIVE"
)

type Entry struct {
	Store     Store
	Key       string
	MediaType string
	Data      []byte
}

type Manifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Engine        Engine          `json:"engine"`
	ResumeSlot    int64           `json:"resumeSlot"`
	Entries       []ManifestEntry `json:"entries"`
}

type ManifestEntry struct {
	Store     Store  `json:"store"`
	Key       string `json:"key"`
	MediaType string `json:"mediaType"`
	Offset    int64  `json:"offset"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

type Bundle struct {
	Manifest Manifest
	Entries  []Entry
}

type Expected struct {
	Engine     Engine
	ResumeSlot int64
}

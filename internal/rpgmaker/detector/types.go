package detector

import (
	"fmt"
	"io"
)

type Generation string

const (
	RPG2000  Generation = "RPG2000"
	RPG2003  Generation = "RPG2003"
	RPGXP    Generation = "RPGXP"
	RPGVX    Generation = "RPGVX"
	RPGVXAce Generation = "RPGVXACE"
	RPGMV    Generation = "RPGMV"
	RPGMZ    Generation = "RPGMZ"
)

type Status string

const (
	Matched    Status = "MATCHED"
	FamilyOnly Status = "FAMILY_ONLY"
)

type Confidence string

const (
	ConfidenceExact      Confidence = "MATCHED"
	ConfidenceFamilyOnly Confidence = "FAMILY_ONLY"
)

const (
	FamilyRPG2K = "RPG2K"
	FamilyRGSS  = "RGSS"
	FamilyMV    = "MV"
	FamilyMZ    = "MZ"
)

type Requirement string

const (
	RequirementRPG2KRTP           Requirement = "RPG2K_RTP_REQUIRED"
	RequirementNativeWebIsolation Requirement = "NATIVE_WEB_ISOLATION_REQUIRED"
)

type Code string

const (
	CodeCoreUnsupported             Code = "RPG_CORE_UNSUPPORTED"
	CodeProjectNotFound             Code = "RPG_PROJECT_NOT_FOUND"
	CodePathCollision               Code = "RPG_PATH_COLLISION"
	CodeGenerationAmbiguous         Code = "RPG_GENERATION_AMBIGUOUS"
	CodeGenerationUnsupported       Code = "RPG_GENERATION_UNSUPPORTED"
	CodeSelectedCoreMismatch        Code = "RPG_SELECTED_CORE_MISMATCH"
	CodeLCFInvalid                  Code = "RPG_LCF_INVALID"
	CodeLCFGenerationUnknown        Code = "RPG_LCF_GENERATION_UNKNOWN"
	CodeLMTInvalid                  Code = "RPG_LMT_INVALID"
	CodeINIInvalid                  Code = "RPG_INI_INVALID"
	CodeINIEncodingUnsupported      Code = "RPG_INI_ENCODING_UNSUPPORTED"
	CodeRGSSGenerationConflict      Code = "RPG_RGSS_GENERATION_CONFLICT"
	CodeWebFormatInvalid            Code = "RPG_WEB_FORMAT_INVALID"
	CodeNativeDependencyUnsupported Code = "RPG_NATIVE_DEPENDENCY_UNSUPPORTED"
	CodeNativeBridgeUnsupported     Code = "RPG_NATIVE_BRIDGE_UNSUPPORTED"
)

type File struct {
	Path string
	Size int64
}

type FileIndex interface {
	Files() []File
	Open(path string) (io.ReadCloser, error)
}

type Profile struct {
	SelectedCoreID     string
	Status             Status
	ExpectedGeneration Generation
	EvidenceGeneration *Generation
	EvidenceFamily     string
	EvidenceConfidence Confidence
	MarkerPaths        []string
	EngineVersion      string
	Requirements       []Requirement
	RTPDependencies    []RTPDependency
	SelfContained      bool
}

type RTPDependency struct {
	Slot           int
	DeclaredName   string
	NormalizedName string
}

type Error struct {
	Code               Code
	ExpectedGeneration Generation
	EvidenceGeneration *Generation
	EvidenceFamily     string
	MarkerPaths        []string
	detail             string
	cause              error
}

func (detectionError *Error) Error() string {
	if detectionError.detail == "" {
		return string(detectionError.Code)
	}
	return fmt.Sprintf("%s: %s", detectionError.Code, detectionError.detail)
}

func (detectionError *Error) Unwrap() error {
	return detectionError.cause
}

func newError(code Code, detail string, cause error) *Error {
	return &Error{Code: code, detail: detail, cause: cause}
}

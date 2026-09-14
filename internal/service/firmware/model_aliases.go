package firmware

import model "retrom/internal/model/firmware"

type (
	ActiveInstallation     = model.ActiveInstallation
	ArchiveInspection      = model.ArchiveInspection
	ArchiveReader          = model.ArchiveReader
	ArchiveWriter          = model.ArchiveWriter
	BlobRecords            = model.BlobRecords
	Consumption            = model.Consumption
	InstallRequest         = model.InstallRequest
	Installation           = model.Installation
	InstallationReader     = model.InstallationReader
	InstallationWrite      = model.InstallationWrite
	InstallationWriter     = model.InstallationWriter
	ReadScope              = model.ReadScope
	ReleaseSignal          = model.ReleaseSignal
	Repository             = model.Repository
	Requirement            = model.Requirement
	RequirementRecords     = model.RequirementRecords
	Selection              = model.Selection
	ServerExecution        = model.ServerExecution
	ServerInstallRequest   = model.ServerInstallRequest
	ServerInstallResult    = model.ServerInstallResult
	ServerOutcome          = model.ServerOutcome
	ServerRecords          = model.ServerRecords
	Upload                 = model.Upload
	UploadReader           = model.UploadReader
	WriteScope             = model.WriteScope
	SupersededInstallation = model.SupersededInstallation
	SupersessionReader     = model.SupersessionReader
	SupersessionScope      = model.SupersessionScope
	SupersessionWriter     = model.SupersessionWriter
)

var (
	ErrInvalid              = model.ErrInvalid
	ErrArchiveFactsNotFound = model.ErrArchiveFactsNotFound
	ErrCatalogChanged       = model.ErrCatalogChanged
)

package datindex

import model "retrom/internal/model/datindex"

type (
	Definition  = model.Definition
	Entry       = model.Entry
	Records     = model.Records
	Requirement = model.Requirement
	Retirement  = model.Retirement
)

var (
	SyncRequirements = model.SyncRequirements
	buildRequirement = model.BuildRequirement
)

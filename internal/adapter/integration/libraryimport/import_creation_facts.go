package libraryimport

import (
	libraryimportmodel "retrom/internal/model/libraryimport"
)

func importTargetFacts(target creationTarget) libraryimportmodel.ImportTarget    { return target }
func legacyCreationTarget(target libraryimportmodel.ImportTarget) creationTarget { return target }
func importFileFacts(files []importSourceFile) []libraryimportmodel.ImportFile   { return files }

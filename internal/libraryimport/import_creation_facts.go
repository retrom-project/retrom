package libraryimport

import application "retrom/internal/service/libraryimport"

func importTargetFacts(target creationTarget) application.ImportTarget    { return target }
func legacyCreationTarget(target application.ImportTarget) creationTarget { return target }
func importFileFacts(files []importSourceFile) []application.ImportFile   { return files }

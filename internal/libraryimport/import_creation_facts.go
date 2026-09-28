package libraryimport

import libraryservice "retrom/internal/service/libraryimport"

func importTargetFacts(target creationTarget) libraryservice.ImportTarget    { return target }
func legacyCreationTarget(target libraryservice.ImportTarget) creationTarget { return target }
func importFileFacts(files []importSourceFile) []libraryservice.ImportFile   { return files }

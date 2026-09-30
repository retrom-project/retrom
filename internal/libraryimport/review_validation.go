package libraryimport

import (
	"retrom/internal/content/arcade"
)

type (
	arcadeDraftDependency = arcade.Dependency
	arcadeDraftSnapshot   = arcade.Snapshot
)

func parseArcadeDraftSnapshot(raw string) (arcadeDraftSnapshot, bool) {
	return arcade.ParseSnapshot(raw)
}

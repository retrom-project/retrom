package cleanup

// Plan describes the bounded cleanup steps chosen by an owning domain.
// The job executor supplies transactions, progress checks and retries.
type Plan struct {
	Groups                  []string
	ConsumeUploads          bool
	RequireChildrenReleased bool
	WaitForMutations        bool
	AdvanceVersion          bool
	AllowAdvancedVersion    bool
}

package emulationstationimport

type TerminalItemCounts struct {
	SkippedMapping  int64
	ReviewPending   int64
	Published       int64
	ReviewDiscarded int64
	Existing        int64
	Blocked         int64
	Failed          int64
	Cancelled       int64
}

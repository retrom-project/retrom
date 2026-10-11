package model

type ROMSetQuery struct {
	CoreID    string
	Search    string
	Offset    int
	Limit     int
	EntryFile string
	ROMSet    *string
}

type ROMSetEntry struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Year         string `json:"year"`
	Manufacturer string `json:"manufacturer"`
}

type ROMSetOptions struct {
	CoreID             string        `json:"coreId"`
	DATSHA256          string        `json:"datSHA256"`
	AutomaticSelection *string       `json:"automaticSelection"`
	Selected           *ROMSetEntry  `json:"selected"`
	Entries            []ROMSetEntry `json:"entries"`
	Total              int           `json:"total"`
	Offset             int           `json:"offset"`
	Limit              int           `json:"limit"`
}

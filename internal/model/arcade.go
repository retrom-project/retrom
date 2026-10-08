package model

type ArcadeParentOptions struct {
	CoreID         string   `json:"coreId"`
	ParentFiles    []string `json:"parentFiles"`
	MissingParents []string `json:"missingParents"`
}

type ArcadeParentError struct {
	Code    string
	Message string
}

func (e *ArcadeParentError) Error() string { return e.Message }
func (e *ArcadeParentError) Unwrap() error { return ErrInvalid }

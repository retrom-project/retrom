package model

type ArcadeParentOptions struct {
	CoreID         string   `json:"coreId"`
	ParentFiles    []string `json:"parentFiles"`
	MissingParents []string `json:"missingParents"`
}

type RuntimeContentError struct {
	Code    string
	Message string
}

func (e *RuntimeContentError) Error() string { return e.Message }
func (e *RuntimeContentError) Unwrap() error { return ErrInvalid }

package payloadrelease

type releaseError struct{ code string }

func (err releaseError) Error() string { return err.code }
func (err releaseError) Code() string  { return err.code }
func releaseFailure(code string) error { return releaseError{code: code} }

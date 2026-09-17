package saves

import (
	"errors"
	"fmt"
	"net/http"
	model "retrom/internal/model/saves"
	"testing"
)

func TestMultipartFailureUsesTypeAndPreservesCause(t *testing.T) {
	for _, test := range []struct {
		cause, kind error
	}{
		{&http.MaxBytesError{Limit: 10}, model.ErrTooLarge},
		{errors.New("request body too large"), model.ErrInvalid},
		{errors.New("stream interrupted"), model.ErrInvalid},
	} {
		err := classifyMultipartError(fmt.Errorf("read multipart: %w", test.cause))
		if !errors.Is(err, test.kind) || !errors.Is(err, test.cause) {
			t.Errorf("classification lost type or cause: got=%v want=%v cause=%v", err, test.kind, test.cause)
		}
	}
}

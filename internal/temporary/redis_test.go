package temporary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"retrom/internal/model"

	"github.com/redis/go-redis/v9"
)

type hashAdmissionHook struct {
	called bool
	err    error
}

func (*hashAdmissionHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (*hashAdmissionHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return next
}

func (h *hashAdmissionHook) ProcessPipelineHook(_ redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(context.Context, []redis.Cmder) error {
		h.called = true
		return h.err
	}
}

func TestRunHashAdmissionSupportsLargeProjectsAndKeepsBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		values   map[string]string
		rejected bool
	}{
		{name: "50000 file references", values: hashFields(50000)},
		{name: "64 MiB", values: hashBytes(64 * 1024 * 1024)},
		{name: "64 MiB plus one", values: hashBytes(64*1024*1024 + 1), rejected: true},
		{name: "100001 fields", values: hashFields(100001), rejected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			transportErr := errors.New("pipeline admitted without contacting a Redis server")
			hook := &hashAdmissionHook{err: transportErr}
			client := redis.NewClient(&redis.Options{MaxRetries: -1})
			t.Cleanup(func() {
				if err := client.Close(); err != nil {
					t.Error(err)
				}
			})
			client.AddHook(hook)
			store := &Redis{client: client, prefix: "test:"}
			err := store.PutHash(t.Context(), "run:project", test.values, time.Minute)
			if test.rejected {
				if !errors.Is(err, model.ErrInvalid) || hook.called {
					t.Fatalf("overflow error=%v pipeline called=%v", err, hook.called)
				}
			} else if !errors.Is(err, transportErr) || !hook.called {
				t.Fatalf("valid large project rejected before Redis: %v", err)
			}
		})
	}
}

func TestOrdinaryTemporaryValueKeepsExistingByteLimit(t *testing.T) {
	t.Parallel()
	err := (&Redis{}).Set(t.Context(), "ordinary", strings.Repeat("x", 8*1024*1024+1), time.Minute)
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("oversized ordinary value: %v", err)
	}
}

func hashFields(count int) map[string]string {
	values := make(map[string]string, count)
	for index := range count {
		values[fmt.Sprintf("file:%05d", index)] = "reference"
	}
	return values
}

func hashBytes(size int) map[string]string {
	const chunk = 1024 * 1024
	payload := strings.Repeat("x", chunk)
	values := make(map[string]string)
	for index := 0; size > 0; index++ {
		field := fmt.Sprintf("field:%02d", index)
		length := min(size-len(field), chunk)
		values[field] = payload[:length]
		size -= len(field) + length
	}
	return values
}

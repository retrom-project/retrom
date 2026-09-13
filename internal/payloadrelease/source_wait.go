package payloadrelease

import (
	"context"
	"fmt"
	"time"
)

func (*Sources) Wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for payload mutation: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

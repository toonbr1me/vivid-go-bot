package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBackgroundTaskSurvivesFailureAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	attempts := 0
	err := every(ctx, "test task", time.Millisecond, func(context.Context) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary failure")
		}
		cancel()
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("background task stopped on transient error or ignored cancellation: %d %v", attempts, err)
	}
}

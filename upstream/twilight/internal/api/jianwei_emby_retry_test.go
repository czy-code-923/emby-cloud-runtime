package api

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestJianweiEmbyRetryRecoversConnectionRefused(t *testing.T) {
	attempts := 0
	err := embyRetryOn5xx(context.Background(), func(context.Context) error {
		attempts++
		if attempts == 1 {
			return errors.New("dial tcp: connect: connection refused")
		}
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("expected recovery after restart: attempts=%d err=%v", attempts, err)
	}
}

func TestJianweiEmbyRetryCancelsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	err := embyRetryOn5xx(ctx, func(context.Context) error {
		cancel()
		return errors.New("connection refused")
	})
	if !errors.Is(err, context.Canceled) || time.Since(started) > time.Second {
		t.Fatalf("cancellation did not interrupt retry: %v", err)
	}
}

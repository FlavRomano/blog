package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunExecutesImmediatelyAndOnTick(t *testing.T) {
	var n atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Run(ctx, 10*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)),
			Task{"count", func(context.Context) error { n.Add(1); return nil }})
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done
	if n.Load() < 3 {
		t.Fatalf("ran %d times", n.Load())
	}
}

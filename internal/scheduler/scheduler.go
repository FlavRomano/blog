// Package scheduler runs periodic background tasks.
package scheduler

import (
	"context"
	"log/slog"
	"time"
)

type Task struct {
	Name string
	Run  func(ctx context.Context) error
}

// Run executes every task immediately and then on each tick until ctx is done.
func Run(ctx context.Context, every time.Duration, log *slog.Logger, tasks ...Task) {
	runAll := func() {
		for _, t := range tasks {
			if err := t.Run(ctx); err != nil && ctx.Err() == nil {
				log.Error("scheduled task failed", "task", t.Name, "err", err)
			}
		}
	}
	runAll()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			runAll()
		}
	}
}

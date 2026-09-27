package sync

import (
	"context"
	"time"

	"github.com/kenners/zotrem/pkg/db"
)

const schedulerTick = 30 * time.Second

func (e *Engine) TriggerAsync(trigger string) bool {
	if e.Running() {
		return false
	}
	go func() {
		if _, err := e.Run(context.Background(), trigger); err != nil {
			e.log.Warn("sync run finished with error", "error", err)
		}
	}()
	return true
}

func (e *Engine) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(schedulerTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if e.due() {
				if _, err := e.Run(ctx, db.TriggerScheduled); err != nil {
					e.log.Warn("scheduled sync finished with error", "error", err)
				}
			}
		}
	}
}

func (e *Engine) due() bool {
	settings, err := e.store.Settings()
	if err != nil || !settings.Configured() || settings.SyncIntervalMinutes <= 0 {
		return false
	}
	latest, err := e.store.LatestSyncRun()
	if err != nil {
		return false
	}
	if latest == nil {
		return true
	}
	return time.Since(latest.StartedAt.Time) >= time.Duration(settings.SyncIntervalMinutes)*time.Minute
}

func (e *Engine) NextRun() (time.Time, bool) {
	settings, err := e.store.Settings()
	if err != nil || !settings.Configured() || settings.SyncIntervalMinutes <= 0 {
		return time.Time{}, false
	}
	latest, err := e.store.LatestSyncRun()
	if err != nil {
		return time.Time{}, false
	}
	if latest == nil {
		return time.Now(), true
	}
	return latest.StartedAt.Add(time.Duration(settings.SyncIntervalMinutes) * time.Minute), true
}

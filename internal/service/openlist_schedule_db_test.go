//go:build cgo

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openListScheduleFixture(t *testing.T) (*AutomaticScheduler, *ScheduledTaskService, *config.Config, time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	// Use SQLite-native datetime/rowid types for the generated MySQL model.
	require.NoError(t, db.Exec(`CREATE TABLE scheduled_task_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT, task_type INTEGER NOT NULL, task_key TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'QUEUED', scheduled_at DATETIME NOT NULL,
		input_json BLOB NOT NULL, output_json BLOB, error_message TEXT NOT NULL DEFAULT '',
		worker_id TEXT NOT NULL DEFAULT '', attempt_count INTEGER NOT NULL DEFAULT 0,
		started_at DATETIME, finished_at DATETIME, config_version INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(task_type, task_key))`).Error)
	cfg := &config.Config{}
	cfg.Meta.Timezone = "Asia/Shanghai"
	cfg.Scheduler.Enabled = true
	cfg.Scheduler.OpenListDocumentIngestion = config.DailyTaskScheduleConfig{Enabled: true, Hour: 5}
	cfg.ExternalDocuments.OpenList.Enabled = true
	tasks := NewScheduledTaskService(db, config.NewRuntime(&config.Snapshot{Config: cfg}), nil)
	now := time.Date(2026, time.November, 6, 5, 0, 0, 0, config.ChinaLocation)
	tasks.now = func() time.Time { return now }
	return &AutomaticScheduler{tasks: tasks}, tasks, cfg, now
}

func TestOpenListDailyEnqueueIsIdempotentAndNeverCatchesUp(t *testing.T) {
	scheduler, tasks, cfg, at := openListScheduleFixture(t)
	ctx := context.Background()
	for _, offset := range []time.Duration{-time.Second, time.Minute, 8 * time.Hour} {
		require.NoError(t, scheduler.enqueueDueTasks(ctx, cfg, at.Add(offset)))
	}
	var count int64
	require.NoError(t, tasks.db.Model(&db_model.ScheduledTaskRun{}).Count(&count).Error)
	require.Zero(t, count)
	for _, offset := range []time.Duration{0, time.Second, 59 * time.Second, 24 * time.Hour} {
		require.NoError(t, scheduler.enqueueDueTasks(ctx, cfg, at.Add(offset)))
	}
	var rows []db_model.ScheduledTaskRun
	require.NoError(t, tasks.db.Order("scheduled_at").Find(&rows).Error)
	require.Len(t, rows, 2)
	for i, row := range rows {
		require.Equal(t, at.AddDate(0, 0, i).Format(time.DateOnly), row.TaskKey)
		require.True(t, row.ScheduledAt.Equal(at.AddDate(0, 0, i)))
		require.JSONEq(t, `{"full_scan":false}`, string(row.InputJSON))
	}
}

func TestOpenListQueuedOrReclaimedTasksCannotStartOutsideDailyPolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		key      string
		atDelta  time.Duration
		nowDelta time.Duration
		attempt  int32
		disabled bool
		reason   string
	}{
		{name: "legacy hourly", key: "2026-11-06T05", reason: "OBSOLETE_OPENLIST_SCHEDULE"},
		{name: "yesterday", key: "2026-11-05", atDelta: -24 * time.Hour, reason: "OBSOLETE_OPENLIST_SCHEDULE"},
		{name: "old schedule", key: "2026-11-06", atDelta: -time.Hour, reason: "OBSOLETE_OPENLIST_SCHEDULE"},
		{name: "delayed queue", key: "2026-11-06", nowDelta: time.Minute, reason: "MISSED_OPENLIST_START_WINDOW"},
		{name: "afternoon recovery", key: "2026-11-06", nowDelta: 8 * time.Hour, attempt: 1, reason: "MISSED_OPENLIST_START_WINDOW"},
		{name: "reclaimed same minute", key: "2026-11-06", attempt: 1, reason: "OPENLIST_DAILY_ATTEMPT_ALREADY_STARTED"},
		{name: "disabled", key: "2026-11-06", disabled: true, reason: "OPENLIST_SCHEDULE_DISABLED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, tasks, cfg, at := openListScheduleFixture(t)
			cfg.Scheduler.OpenListDocumentIngestion.Enabled = !test.disabled
			now := at.Add(test.nowDelta)
			// UTC clocks must still honor the Beijing date and start minute.
			tasks.now = func() time.Time { return now.UTC() }
			calls := 0
			require.NoError(t, tasks.RegisterHandler(domain.ScheduledTaskTypeOpenListDocuments, func(context.Context, json.RawMessage) (any, error) {
				calls++
				return nil, nil
			}))
			_, row, err := tasks.Enqueue(context.Background(), domain.ScheduledTaskTypeOpenListDocuments, test.key, at.Add(test.atDelta), OpenListDocumentIngestionRequest{})
			require.NoError(t, err)
			if test.attempt > 0 {
				started := now.Add(-9 * time.Hour).UTC()
				require.NoError(t, tasks.db.Model(row).Updates(map[string]any{
					"status": ScheduledTaskStatusRunning, "attempt_count": test.attempt, "started_at": started,
				}).Error)
			}
			claimed, err := tasks.ClaimAndExecuteNext(context.Background(), "test", 8*time.Hour, domain.ScheduledTaskTypeOpenListDocuments)
			require.NoError(t, err)
			require.True(t, claimed)
			require.Zero(t, calls, "a skipped task must not invoke ingestion or the model")
			require.NoError(t, tasks.db.First(row, row.ID).Error)
			require.Equal(t, ScheduledTaskStatusSkipped, row.Status)
			require.Contains(t, string(row.OutputJSON), test.reason)
			require.NotNil(t, row.FinishedAt)
		})
	}
}

func TestOpenListDailyFailureWaitsForNextDate(t *testing.T) {
	scheduler, tasks, cfg, at := openListScheduleFixture(t)
	calls := 0
	require.NoError(t, tasks.RegisterHandler(domain.ScheduledTaskTypeOpenListDocuments, func(context.Context, json.RawMessage) (any, error) {
		calls++
		return nil, errors.New("upstream unavailable")
	}))
	ctx := context.Background()
	require.NoError(t, scheduler.enqueueDueTasks(ctx, cfg, at))
	claimed, err := tasks.ClaimAndExecuteNext(ctx, "test", time.Hour)
	require.True(t, claimed)
	require.EqualError(t, err, "upstream unavailable")
	require.NoError(t, scheduler.enqueueDueTasks(ctx, cfg, at.Add(30*time.Second)))
	claimed, err = tasks.ClaimAndExecuteNext(ctx, "test", time.Hour)
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, 1, calls)
	next := at.AddDate(0, 0, 1)
	tasks.now = func() time.Time { return next }
	require.NoError(t, scheduler.enqueueDueTasks(ctx, cfg, next))
	claimed, err = tasks.ClaimAndExecuteNext(ctx, "test", time.Hour)
	require.True(t, claimed)
	require.EqualError(t, err, "upstream unavailable")
	require.Equal(t, 2, calls)
}

func TestOpenListStartedBatchCanFinishAfterStartMinute(t *testing.T) {
	scheduler, tasks, cfg, at := openListScheduleFixture(t)
	require.NoError(t, tasks.RegisterHandler(domain.ScheduledTaskTypeOpenListDocuments, func(context.Context, json.RawMessage) (any, error) {
		tasks.now = func() time.Time { return at.Add(2 * time.Hour) }
		return map[string]int{"processed_count": 1}, nil
	}))
	require.NoError(t, scheduler.enqueueDueTasks(context.Background(), cfg, at))
	claimed, err := tasks.ClaimAndExecuteNext(context.Background(), "test", 8*time.Hour)
	require.NoError(t, err)
	require.True(t, claimed)
	var row db_model.ScheduledTaskRun
	require.NoError(t, tasks.db.First(&row).Error)
	require.Equal(t, ScheduledTaskStatusSucceeded, row.Status)
}

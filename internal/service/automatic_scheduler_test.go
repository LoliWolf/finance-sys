package service

import (
	"testing"
	"time"
)

func TestDailyScheduledAtUsesConfiguredTimezoneAndClock(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 26, 15, 30, 0, 0, location)
	got := dailyScheduledAt(now, 2, 0)
	want := time.Date(2026, time.July, 26, 2, 0, 0, 0, location)
	if !got.Equal(want) {
		t.Fatalf("scheduled time = %s, want %s", got, want)
	}
}

func TestWithinScheduledMinuteDoesNotCatchUp(t *testing.T) {
	scheduledAt := time.Date(2026, time.November, 6, 5, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	for _, test := range []struct {
		offset time.Duration
		want   bool
	}{
		{-time.Nanosecond, false},
		{0, true},
		{time.Minute - time.Nanosecond, true},
		{time.Minute, false},
		{8 * time.Hour, false},
		{24 * time.Hour, false},
	} {
		if got := withinScheduledMinute(scheduledAt.Add(test.offset), scheduledAt); got != test.want {
			t.Errorf("offset %s: got %v, want %v", test.offset, got, test.want)
		}
	}
}

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

func TestHourlyScheduledAtUsesCurrentHourAndConfiguredMinute(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 26, 15, 30, 0, 0, location)
	got := hourlyScheduledAt(now, 15)
	want := time.Date(2026, time.July, 26, 15, 15, 0, 0, location)
	if !got.Equal(want) {
		t.Fatalf("scheduled time = %s, want %s", got, want)
	}
}

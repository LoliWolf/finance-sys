package config

import (
	"testing"
	"time"
)

func TestTradingCalendarCoverageHolidaysAndElapsed(t *testing.T) {
	c := TradingCalendarConfig{Source: "fixture", Version: "1", CoverageFrom: "2026-09-01", CoverageTo: "2026-10-09", OpenDates: []string{"2026-09-24", "2026-09-28", "2026-09-29", "2026-09-30", "2026-10-08", "2026-10-09"}}
	parse := func(s string) time.Time { d, _ := time.ParseInLocation(time.DateOnly, s, ChinaLocation); return d }
	if open, err := c.IsOpen(parse("2026-10-01")); err != nil || open {
		t.Fatalf("holiday open=%v err=%v", open, err)
	}
	if next, err := c.Next(parse("2026-09-30")); err != nil || ChinaDate(next) != "2026-10-08" {
		t.Fatalf("bad next day %v %v", next, err)
	}
	if days, err := c.Elapsed(parse("2026-09-30"), parse("2026-10-08")); err != nil || days != 1 {
		t.Fatalf("bad holding days %d %v", days, err)
	}
	if _, err := c.IsOpen(parse("2027-01-01")); err == nil {
		t.Fatal("unknown calendar must fail closed")
	}
	if _, err := c.Next(parse("2026-10-09")); err == nil {
		t.Fatal("must not invent next weekday")
	}
	c.OpenDates = append(c.OpenDates, "2026-10-09")
	if c.Validate() == nil {
		t.Fatal("duplicate calendar date accepted")
	}
}

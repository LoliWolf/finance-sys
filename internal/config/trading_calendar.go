package config

import (
	"fmt"
	"sort"
	"time"
)

var ChinaLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func ChinaDate(at time.Time) string { return at.In(ChinaLocation).Format(time.DateOnly) }

func (c TradingCalendarConfig) Validate() error {
	from, err := time.Parse(time.DateOnly, c.CoverageFrom)
	if err != nil {
		return fmt.Errorf("calendar coverage_from is invalid")
	}
	to, err := time.Parse(time.DateOnly, c.CoverageTo)
	if err != nil || to.Before(from) || c.Source == "" || c.Version == "" || len(c.OpenDates) == 0 {
		return fmt.Errorf("calendar provenance or coverage is invalid")
	}
	previous := ""
	for _, date := range c.OpenDates {
		d, err := time.Parse(time.DateOnly, date)
		if err != nil || d.Before(from) || d.After(to) || date <= previous || d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			return fmt.Errorf("calendar open dates must be unique, sorted and inside coverage")
		}
		previous = date
	}
	return nil
}

func (c TradingCalendarConfig) IsOpen(at time.Time) (bool, error) {
	if err := c.Validate(); err != nil {
		return false, err
	}
	d := ChinaDate(at)
	if d < c.CoverageFrom || d > c.CoverageTo {
		return false, fmt.Errorf("calendar does not cover %s", d)
	}
	i := sort.SearchStrings(c.OpenDates, d)
	return i < len(c.OpenDates) && c.OpenDates[i] == d, nil
}

func (c TradingCalendarConfig) Next(at time.Time) (time.Time, error) {
	if _, err := c.IsOpen(at); err != nil {
		return time.Time{}, err
	}
	i := sort.Search(len(c.OpenDates), func(i int) bool { return c.OpenDates[i] > ChinaDate(at) })
	if i == len(c.OpenDates) {
		return time.Time{}, fmt.Errorf("next trading day is outside calendar coverage")
	}
	return time.ParseInLocation(time.DateOnly, c.OpenDates[i], ChinaLocation)
}

// Elapsed excludes the entry date and includes the as-of date, independent of quotes.
func (c TradingCalendarConfig) Elapsed(entry, asOf time.Time) (int, error) {
	if _, err := c.IsOpen(entry); err != nil {
		return 0, err
	}
	if _, err := c.IsOpen(asOf); err != nil {
		return 0, err
	}
	from, to := ChinaDate(entry), ChinaDate(asOf)
	if to < from {
		return 0, fmt.Errorf("as-of precedes entry")
	}
	first := sort.Search(len(c.OpenDates), func(i int) bool { return c.OpenDates[i] > from })
	last := sort.Search(len(c.OpenDates), func(i int) bool { return c.OpenDates[i] > to })
	return last - first, nil
}

func (c TradingCalendarConfig) Previous(at time.Time, offset int) (time.Time, error) {
	if _, err := c.IsOpen(at); err != nil {
		return time.Time{}, err
	}
	i := sort.SearchStrings(c.OpenDates, ChinaDate(at)) - offset
	if i < 0 || i >= len(c.OpenDates) {
		return time.Time{}, fmt.Errorf("cooldown exceeds calendar coverage")
	}
	return time.ParseInLocation(time.DateOnly, c.OpenDates[i], ChinaLocation)
}

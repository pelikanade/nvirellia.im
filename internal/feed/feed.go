// Package feed builds the nvirellia.im feed: token windows from magpie. Tokens is
// nil while magpie has no data (unconfigured, unreachable, or no buckets).
package feed

import (
	"time"
	_ "time/tzdata" // the day zone must resolve in minimal containers
)

const zoneName = "Asia/Shanghai"

// Layouts for the daily and hourly keys. Both are read in the feed zone (UTC+8).
const (
	dayLayout  = "2006-01-02"
	hourLayout = "2006-01-02T15"
)

// Location returns the feed's day zone, UTC+8.
func Location() (*time.Location, error) {
	return time.LoadLocation(zoneName)
}

// Feed is the nvirellia.im profile feed: generation time, day zone, and token windows.
type Feed struct {
	GeneratedAt string  `json:"generatedAt"`
	TZ          string  `json:"tz"`
	Tokens      *Tokens `json:"tokens"`
}

// Tokens is the magpie-derived part of the feed.
type Tokens struct {
	Today  Pair   `json:"today"`
	Hourly Hourly `json:"hourly"`
	Daily  Daily  `json:"daily"`
}

// Pair is input and output token counts.
type Pair struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}

// Hourly is today's 24 local hours, oldest first; hours after now are zero.
type Hourly struct {
	Start  string  `json:"start"`
	Input  []int64 `json:"input"`
	Output []int64 `json:"output"`
}

// Daily is 16 Monday-first weeks; nil entries are days after today, and days before magpie's first record are zero.
type Daily struct {
	Start  string   `json:"start"`
	Input  []*int64 `json:"input"`
	Output []*int64 `json:"output"`
}

// Bucket is magpie usage for one local hour or day, keyed by its start instant.
type Bucket struct {
	Start  time.Time `json:"start"`
	Input  int64     `json:"input"`
	Output int64     `json:"output"`
}

// Usage is magpie's token usage as the feed reads it: today's hourly buckets and
// the per-day buckets from magpie's first usage record.
type Usage struct {
	Hours []Bucket
	Days  []Bucket
}

// Window sizes shared by aggregation and the daily layout.
const (
	hourlyWindow = 24
	dailyWeeks   = 16
	dailyDays    = 7 * dailyWeeks
)

// weekdayIndex maps a time to its Monday-first index: Mon=0 … Sun=6.
func weekdayIndex(t time.Time) int {
	return (int(t.Weekday()) + 6) % 7
}

// dailyStart is local midnight of the Monday that opens the 16-week window.
func dailyStart(local time.Time) time.Time {
	y, m, d := local.Date()
	return time.Date(y, m, d-weekdayIndex(local)-7*(dailyWeeks-1), 0, 0, 0, 0, local.Location())
}

package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/pelikanade/nvirellia.im/internal/feed"
)

// page is the template data for the sheet. Field names are used by templates/*.html.
type page struct {
	Resources bool // magpie has data: the Resources tab, its panel, and the status readouts render
	FeedJSON  template.JS
	Updated   string
	Readouts  readouts
	Legend    legend
}

type readouts struct {
	Today, Input, Output, ThisHour, Peak, Last7d, Note string
}

type legend struct {
	In, Out string
}

// newPage derives everything the templates show from one feed snapshot.
func newPage(f feed.Feed) (page, error) {
	generated, err := time.Parse(time.RFC3339, f.GeneratedAt)
	if err != nil {
		return page{}, fmt.Errorf("generatedAt: %w", err)
	}
	feedJSON, err := json.Marshal(f)
	if err != nil {
		return page{}, err
	}
	p := page{FeedJSON: template.JS(feedJSON), Updated: generated.Format("2006-01-02")}
	if f.Tokens == nil {
		return p, nil
	}
	hourStart, err := time.Parse(time.RFC3339, f.Tokens.Hourly.Start)
	if err != nil {
		return page{}, fmt.Errorf("hourly start: %w", err)
	}

	t := *f.Tokens
	hourTot := make([]int64, len(t.Hourly.Input))
	for i := range hourTot {
		hourTot[i] = t.Hourly.Input[i] + t.Hourly.Output[i]
	}
	dayTot := make([]*int64, len(t.Daily.Input))
	last := 0
	for i := range dayTot {
		if t.Daily.Input[i] == nil {
			continue
		}
		v := *t.Daily.Input[i] + *t.Daily.Output[i]
		dayTot[i] = &v
		last = i
	}

	nowH := generated.Hour() // hourly starts at local midnight, so the index is the hour
	peak := slices.Index(hourTot, slices.Max(hourTot))
	var last7 int64
	for i := max(0, last-6); i <= last; i++ {
		if dayTot[i] != nil {
			last7 += *dayTot[i]
		}
	}
	todayTot := t.Today.Input + t.Today.Output

	snapshotLabel := generated.Format("2006-01-02 15:04") + " (UTC+8)"
	p.Resources = true
	p.Readouts = readouts{
		Today:    fmtTokens(todayTot),
		Input:    fmtTokens(t.Today.Input),
		Output:   fmtTokens(t.Today.Output),
		ThisHour: fmtTokens(hourTot[nowH]),
		Peak:     fmt.Sprintf("%s · %s", hourLabel(hourStart, peak), fmtTokens(hourTot[peak])),
		Last7d:   fmtTokens(last7),
		Note:     fmt.Sprintf("magpie snapshot %s; \"this hour\" is still filling.", snapshotLabel),
	}
	p.Legend = legend{
		In:  fmt.Sprintf("today %s · this hour %s", fmtTokens(t.Today.Input), fmtTokens(t.Hourly.Input[nowH])),
		Out: fmt.Sprintf("today %s · this hour %s", fmtTokens(t.Today.Output), fmtTokens(t.Hourly.Output[nowH])),
	}
	return p, nil
}

// hourLabel is the "HH:00" label of the hour at index i past the window start.
func hourLabel(start time.Time, i int) string {
	return fmt.Sprintf("%02d:00", (start.Hour()+i)%24)
}

// fmtTokens matches the page script's readout format: 4477200 -> "4.5M", 182300 -> "182k", 4710 -> "4.7k".
func fmtTokens(n int64) string {
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1e4:
		return fmt.Sprintf("%dk", int64(math.Round(float64(n)/1e3)))
	case n >= 1e3:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return strconv.FormatInt(n, 10)
	}
}

// icon is the sprite reference for id, a fixed literal from the templates.
func icon(id string) template.HTML {
	return template.HTML(`<svg class="ic" aria-hidden="true"><use href="#` + id + `"/></svg>`)
}

package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pelikanade/nvirellia.im/internal/feed"
)

// page is the template data for the sheet. Field names are used by templates/*.html.
type page struct {
	Resources bool // magpie has data: the Resources tab, its panel, and the status readouts render
	Status    *statusView
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

// statusView is the Status panel, already formatted for the template.
type statusView struct {
	Uptime *uptimeView
	Probes *probeView
}

type uptimeView struct {
	Endpoints []uptimeRow
}

const uptimeBarSegments = 50

type uptimeRow struct {
	Name, Group, State, Uptime24h, Uptime7d, Last, Oldest, BarLabel string
	Checks                                                          []checkView
}

type checkView struct {
	State, Title string
}

type probeView struct {
	Systems []probeRow
}

type probeRow struct {
	Name, Host, State, Uptime, Seen string
	Metrics                         []metricView
}

// metricView is one host metric: its current value, a 0..100 meter width, and
// the SVG points for its history sparkline.
type metricView struct {
	Class, Label, Value, Width, Points, Area, Aria string
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
	loc, err := time.LoadLocation(f.TZ)
	if err != nil {
		loc = generated.Location()
	}
	p.Status = newStatusView(f.Status, loc)
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
		Note:     fmt.Sprintf("magpie snapshot: %s", snapshotLabel),
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

// newStatusView formats gatus and Prometheus data for the Status panel. It
// returns nil when neither source has anything to show, so the tab stays out of
// the page.
func newStatusView(s *feed.Status, loc *time.Location) *statusView {
	if s == nil {
		return nil
	}
	v := &statusView{}
	if s.Uptime != nil {
		v.Uptime = &uptimeView{}
		for _, e := range s.Uptime.Endpoints {
			checks, label, oldest := uptimeBar(e, loc)
			v.Uptime.Endpoints = append(v.Uptime.Endpoints, uptimeRow{
				Name:      e.Name,
				Group:     e.Group,
				State:     endpointState(e),
				Uptime24h: fmtPercent(e.Uptime24h),
				Uptime7d:  fmtPercent(e.Uptime7d),
				Last:      lastCheck(e, loc),
				Oldest:    oldest,
				BarLabel:  label,
				Checks:    checks,
			})
		}
	}
	if s.Probes != nil {
		v.Probes = &probeView{}
		for _, p := range s.Probes.Systems {
			row := probeRow{
				Name:  p.Name,
				Host:  p.Host,
				State: probeState(p.Status),
				Seen:  fmtSeen(p.Updated, loc),
				Metrics: []metricView{
					newMetric("cpu", "cpu", p.CPU, p.CPUHistory),
					newMetric("mem", "memory", p.Mem, p.MemHistory),
					newMetric("disk", "disk", p.Disk, p.DiskHistory),
				},
			}
			if p.UptimeSeconds != nil {
				row.Uptime = fmtDuration(*p.UptimeSeconds)
			}
			v.Probes.Systems = append(v.Probes.Systems, row)
		}
	}
	if v.Uptime == nil && v.Probes == nil {
		return nil
	}
	return v
}

// uptimeBar turns the recent Gatus results into one segment per check, padding
// short histories with unknown segments so every row has the same width.
func uptimeBar(e feed.Endpoint, loc *time.Location) (checks []checkView, label, oldest string) {
	results := e.Results
	if len(results) > uptimeBarSegments {
		results = results[len(results)-uptimeBarSegments:]
	}
	checks = make([]checkView, 0, uptimeBarSegments)
	for i := len(results); i < uptimeBarSegments; i++ {
		checks = append(checks, checkView{State: "unknown", Title: "no check"})
	}
	up := 0
	for _, r := range results {
		state := "down"
		if r.Up {
			state, up = "up", up+1
		}
		checks = append(checks, checkView{State: state, Title: checkTitle(r, loc)})
	}
	oldest = "—"
	if len(results) > 0 {
		oldest = checkTime(results[0].At, loc)
		label = fmt.Sprintf("%d of %d checks up", up, len(results))
	} else {
		label = "no checks yet"
	}
	return checks, label, oldest
}

// checkTitle is the native tooltip for one bar segment.
func checkTitle(c feed.Check, loc *time.Location) string {
	parts := []string{checkTime(c.At, loc)}
	if c.Up {
		parts = append(parts, "up")
	} else {
		parts = append(parts, "down")
	}
	if c.Status > 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", c.Status))
	}
	if c.LatencyMS > 0 {
		parts = append(parts, fmt.Sprintf("%dms", c.LatencyMS))
	}
	return strings.Join(parts, " · ")
}

// checkTime formats a check timestamp in the feed zone.
func checkTime(s string, loc *time.Location) string {
	if s == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "—"
	}
	return t.In(loc).Format("15:04")
}

// newMetric formats one host metric and, when enough samples exist, its
// sparkline.
func newMetric(class, label string, v *float64, history []float64) metricView {
	m := metricView{Class: class, Label: label, Value: "—"}
	if v != nil {
		m.Value = fmtPercentValue(*v)
		m.Width = fmt.Sprintf("%.0f", *v)
	}
	if len(history) >= 2 {
		m.Points, m.Area = sparkline(history)
		lo, hi := slices.Min(history), slices.Max(history)
		m.Aria = fmt.Sprintf("%s over the last %d hours: now %s, low %.0f%%, high %.0f%%",
			label, int(feed.ProbeHistoryWindow.Hours()), m.Value, lo, hi)
	}
	return m
}

// sparkline maps percentages to an SVG polyline and its filled area. The
// vertical scale follows the series, with a ten-point floor, so a quiet CPU
// still shows its shape instead of hugging the baseline.
func sparkline(values []float64) (points, area string) {
	if len(values) < 2 {
		return "", ""
	}
	lo, hi := slices.Min(values), slices.Max(values)
	if hi-lo < 10 {
		mid := (lo + hi) / 2
		lo, hi = max(0, mid-5), min(100, mid+5)
		if hi <= lo {
			hi = lo + 1
		}
	}
	var b strings.Builder
	for i, v := range values {
		x := float64(i) / float64(len(values)-1) * 100
		y := 22 - (v-lo)/(hi-lo)*20
		fmt.Fprintf(&b, "%.1f,%.1f ", x, y)
	}
	points = strings.TrimSpace(b.String())
	return points, "0,24 " + points + " 100,24"
}

// endpointState is the gatus monitor state shown on the Status row.
func endpointState(e feed.Endpoint) string {
	switch {
	case !e.Known:
		return "unknown"
	case e.Up:
		return "up"
	default:
		return "down"
	}
}

// probeState keeps Prometheus' states and maps anything else to unknown.
func probeState(s string) string {
	switch s {
	case "up", "down":
		return s
	default:
		return "unknown"
	}
}

// fmtPercent renders gatus' 0..1-derived percentage; nil means no data for the window.
func fmtPercent(p *float64) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f%%", *p)
}

// fmtPercentValue renders a Prometheus percentage.
func fmtPercentValue(v float64) string {
	return fmt.Sprintf("%.0f%%", v)
}

// fmtDuration renders a host uptime in the largest two units that fit.
func fmtDuration(sec uint64) string {
	if sec == 0 {
		return "—"
	}
	d, h, m := sec/86400, (sec%86400)/3600, (sec%3600)/60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// lastCheck is the gatus result time in the feed zone, with its latency.
func lastCheck(e feed.Endpoint, loc *time.Location) string {
	if e.CheckedAt == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, e.CheckedAt)
	if err != nil {
		return "—"
	}
	s := t.In(loc).Format("15:04")
	if e.LatencyMS > 0 {
		s += fmt.Sprintf(" · %dms", e.LatencyMS)
	}
	return s
}

// fmtSeen is a beszel record's last update time in the feed zone.
func fmtSeen(s string, loc *time.Location) string {
	if s == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "—"
	}
	return t.In(loc).Format("15:04")
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

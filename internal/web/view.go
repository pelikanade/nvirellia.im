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

type uptimeRow struct {
	Name, Group, State, Uptime24h, Uptime7d, Last string
}

type probeView struct {
	Systems []probeRow
}

type probeRow struct {
	Name, Host, State, Uptime, CPU, Mem, Disk, Seen string
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

// newStatusView formats gatus and beszel data for the Status panel. It returns
// nil when neither source has anything to show, so the tab stays out of the page.
func newStatusView(s *feed.Status, loc *time.Location) *statusView {
	if s == nil {
		return nil
	}
	v := &statusView{}
	if s.Uptime != nil {
		v.Uptime = &uptimeView{}
		for _, e := range s.Uptime.Endpoints {
			v.Uptime.Endpoints = append(v.Uptime.Endpoints, uptimeRow{
				Name:      e.Name,
				Group:     e.Group,
				State:     endpointState(e),
				Uptime24h: fmtPercent(e.Uptime24h),
				Uptime7d:  fmtPercent(e.Uptime7d),
				Last:      lastCheck(e, loc),
			})
		}
	}
	if s.Probes != nil {
		v.Probes = &probeView{}
		for _, p := range s.Probes.Systems {
			row := probeRow{
				Name:   p.Name,
				Host:   p.Host,
				State:  probeState(p.Status),
				Uptime: "—",
				CPU:    "—",
				Mem:    "—",
				Disk:   "—",
				Seen:   fmtSeen(p.Updated, loc),
			}
			if p.UptimeSeconds != nil {
				row.Uptime = fmtDuration(*p.UptimeSeconds)
			}
			if p.CPU != nil {
				row.CPU = fmtPercentValue(*p.CPU)
			}
			if p.Mem != nil {
				row.Mem = fmtPercentValue(*p.Mem)
			}
			if p.Disk != nil {
				row.Disk = fmtPercentValue(*p.Disk)
			}
			v.Probes.Systems = append(v.Probes.Systems, row)
		}
	}
	if v.Uptime == nil && v.Probes == nil {
		return nil
	}
	return v
}

// endpointState is the gatus monitor state shown in the table.
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

// fmtPercentValue renders a beszel percentage.
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

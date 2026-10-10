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
	Live         bool
	FeedJSON     template.JS
	Updated      string
	RunningCount int
	Readouts     readouts
	Legend       legend
	Agents       []agentRow
	AgentSummary string
	AgentSource  string
}

type readouts struct {
	Today, Input, Output, ThisHour, Peak, Last7d, Running, Note string
}

type legend struct {
	In, Out string
}

type agentRow struct {
	Name, Status, Task, Uptime string
	UptimeSec                  int64
}

var statusRank = map[string]int{"running": 0, "sleeping": 1, "idle": 2}

// newPage derives everything the templates show from one feed snapshot.
func newPage(f feed.Feed) (page, error) {
	generated, err := time.Parse(time.RFC3339, f.GeneratedAt)
	if err != nil {
		return page{}, fmt.Errorf("generatedAt: %w", err)
	}
	hourStart, err := time.Parse(time.RFC3339, f.Tokens.Hourly.Start)
	if err != nil {
		return page{}, fmt.Errorf("hourly start: %w", err)
	}
	feedJSON, err := json.Marshal(f)
	if err != nil {
		return page{}, err
	}

	t := f.Tokens
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

	nowH := len(hourTot) - 1
	peak := slices.Index(hourTot, slices.Max(hourTot))
	var last7 int64
	for i := max(0, last-6); i <= last; i++ {
		if dayTot[i] != nil {
			last7 += *dayTot[i]
		}
	}
	todayTot := t.Today.Input + t.Today.Output

	agents := slices.Clone(f.Agents)
	slices.SortFunc(agents, compareAgents)
	rows := make([]agentRow, 0, len(agents))
	var running, sleeping, idle int
	for _, a := range agents {
		switch a.Status {
		case "running":
			running++
		case "sleeping":
			sleeping++
		case "idle":
			idle++
		}
		rows = append(rows, agentRow{Name: a.Name, Status: a.Status, Task: a.Task,
			UptimeSec: a.UptimeSec, Uptime: uptime(a.UptimeSec)})
	}

	snapshotLabel := generated.Format("2006-01-02 15:04") + " (UTC+8)"
	source := "magpie"
	if f.Sample {
		source = "sample"
	}
	agentSource := "agents from paseo · snapshot " + snapshotLabel
	if f.Sample {
		agentSource = "agents from paseo · sample snapshot " + snapshotLabel
	}

	return page{
		Live:         !f.Sample,
		FeedJSON:     template.JS(feedJSON),
		Updated:      generated.Format("2006-01-02"),
		RunningCount: running,
		Readouts: readouts{
			Today:    fmtTokens(todayTot),
			Input:    fmtTokens(t.Today.Input),
			Output:   fmtTokens(t.Today.Output),
			ThisHour: fmtTokens(hourTot[nowH]),
			Peak:     fmt.Sprintf("%s · %s", hourLabel(hourStart, peak), fmtTokens(hourTot[peak])),
			Last7d:   fmtTokens(last7),
			Running:  fmt.Sprintf("%d of %d", running, len(f.Agents)),
			Note:     fmt.Sprintf("%s snapshot %s; \"this hour\" is still filling.", source, snapshotLabel),
		},
		Legend: legend{
			In:  fmt.Sprintf("today %s · this hour %s", fmtTokens(t.Today.Input), fmtTokens(t.Hourly.Input[nowH])),
			Out: fmt.Sprintf("today %s · this hour %s", fmtTokens(t.Today.Output), fmtTokens(t.Hourly.Output[nowH])),
		},
		Agents:       rows,
		AgentSummary: fmt.Sprintf("%d running · %d sleeping · %d idle", running, sleeping, idle),
		AgentSource:  agentSource,
	}, nil
}

// compareAgents is the default table order: running first, then longest uptime, then name.
func compareAgents(a, b feed.Agent) int {
	if d := rank(a.Status) - rank(b.Status); d != 0 {
		return d
	}
	if a.UptimeSec != b.UptimeSec {
		if a.UptimeSec > b.UptimeSec {
			return -1
		}
		return 1
	}
	return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
}

func rank(status string) int {
	if r, ok := statusRank[status]; ok {
		return r
	}
	return len(statusRank)
}

// hourLabel is the "HH:00" label of the hour at index i past the window start.
func hourLabel(start time.Time, i int) string {
	return fmt.Sprintf("%02d:00", (start.Hour()+i)%24)
}

// fmtTokens matches the monitor's readout format: 4477200 -> "4.5M", 182300 -> "182k", 4710 -> "4.7k".
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

// uptime matches the monitor's uptime column: "2d 5h", "3h 07m", "42m".
func uptime(sec int64) string {
	d := sec / 86400
	h := sec % 86400 / 3600
	m := sec % 3600 / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

package feed

import (
	"slices"
	"time"
)

// sampleTokens lays the prototype's fake token numbers onto the current UTC+8
// windows. Used whenever magpie has no usable data.
func sampleTokens(now time.Time, loc *time.Location) Tokens {
	local := now.In(loc)
	y, m, d := local.Date()
	h := local.Hour()
	todayIdx := 7*(dailyWeeks-1) + weekdayIndex(local)

	t := Tokens{
		Today: Pair{Input: sampleTodayInput, Output: sampleTodayOutput},
		Hourly: Hourly{
			Start:  time.Date(y, m, d, h-(hourlyWindow-1), 0, 0, 0, loc).Format(time.RFC3339),
			Input:  slices.Clone(sampleHourlyInput),
			Output: slices.Clone(sampleHourlyOutput),
		},
		Daily: Daily{Start: dailyStart(local).Format(dayLayout)},
	}
	for i := range dailyDays {
		if i > todayIdx {
			t.Daily.Input = append(t.Daily.Input, nil)
			t.Daily.Output = append(t.Daily.Output, nil)
			continue
		}
		in, out := sampleTodayInput, sampleTodayOutput
		if i < todayIdx {
			in, out = sampleAt(sampleDailyInput, i), sampleAt(sampleDailyOutput, i)
		}
		t.Daily.Input = append(t.Daily.Input, &in)
		t.Daily.Output = append(t.Daily.Output, &out)
	}
	return t
}

// sampleAt returns the i-th sample value, or 0 past the end of the prototype data.
func sampleAt(xs []int64, i int) int64 {
	if i < len(xs) {
		return xs[i]
	}
	return 0
}

// sampleAgents is the prototype's agent list (fake, public-safe task one-liners).
func sampleAgents() []Agent {
	return []Agent{
		{Name: "Designy", Status: "running", UptimeSec: 8040, Task: "rendering prototype screenshots"},
		{Name: "Researchy", Status: "running", UptimeSec: 18120, Task: "reading sources for a design note"},
		{Name: "Sitey", Status: "sleeping", UptimeSec: 108000, Task: "waiting for the next site build"},
		{Name: "Flaky", Status: "running", UptimeSec: 2460, Task: "checking a flake input update"},
		{Name: "Todey", Status: "idle", UptimeSec: 43200, Task: "no task"},
		{Name: "Mailly", Status: "sleeping", UptimeSec: 266400, Task: "waiting for new mail"},
		{Name: "Hassy", Status: "idle", UptimeSec: 529200, Task: "no task"},
		{Name: "VaultKeepy", Status: "sleeping", UptimeSec: 68400, Task: "tidy pass queued"},
		{Name: "Appy", Status: "running", UptimeSec: 5220, Task: "building an app preview"},
	}
}

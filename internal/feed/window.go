package feed

import "time"

// aggregate turns magpie usage into the feed's UTC+8 windows as of now. "Today"
// is the local day's total; hourly is today's 24 local hours from midnight, with
// hours after now zero; daily is the 16-week Monday-first grid, with days after
// today left null.
func aggregate(u Usage, now time.Time, loc *time.Location) Tokens {
	local := now.In(loc)
	y, m, d := local.Date()

	hourly := make(map[string]Pair)
	for _, b := range u.Hours {
		k := b.Start.In(loc).Format(hourLayout)
		hp := hourly[k]
		hp.Input += b.Input
		hp.Output += b.Output
		hourly[k] = hp
	}
	daily := make(map[string]Pair)
	for _, b := range u.Days {
		k := b.Start.In(loc).Format(dayLayout)
		dp := daily[k]
		dp.Input += b.Input
		dp.Output += b.Output
		daily[k] = dp
	}

	t := Tokens{
		Today: daily[local.Format(dayLayout)],
		Hourly: Hourly{
			Start:  time.Date(y, m, d, 0, 0, 0, 0, loc).Format(time.RFC3339),
			Input:  make([]int64, hourlyWindow),
			Output: make([]int64, hourlyWindow),
		},
	}
	for i := range hourlyWindow {
		p := hourly[time.Date(y, m, d, i, 0, 0, 0, loc).Format(hourLayout)]
		t.Hourly.Input[i] = p.Input
		t.Hourly.Output[i] = p.Output
	}

	start := dailyStart(local)
	todayMidnight := time.Date(y, m, d, 0, 0, 0, 0, loc)
	t.Daily.Start = start.Format(dayLayout)
	for i := range dailyDays {
		day := time.Date(start.Year(), start.Month(), start.Day()+i, 0, 0, 0, 0, loc)
		if day.After(todayMidnight) {
			t.Daily.Input = append(t.Daily.Input, nil)
			t.Daily.Output = append(t.Daily.Output, nil)
			continue
		}
		p := daily[day.Format(dayLayout)]
		in, out := p.Input, p.Output
		t.Daily.Input = append(t.Daily.Input, &in)
		t.Daily.Output = append(t.Daily.Output, &out)
	}
	return t
}

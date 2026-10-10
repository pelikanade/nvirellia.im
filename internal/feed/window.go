package feed

import "time"

// aggregate turns hourly magpie buckets into the feed's UTC+8 windows as of now.
// "Today" is the local day; hourly ends at the current local hour; daily is the
// 16-week Monday-first grid, with days after today left null.
func aggregate(buckets []Bucket, now time.Time, loc *time.Location) Tokens {
	local := now.In(loc)
	y, m, d := local.Date()
	h := local.Hour()

	hourly := make(map[string]Pair)
	daily := make(map[string]Pair)
	for _, b := range buckets {
		bt := b.Start.In(loc)
		hk, dk := bt.Format(hourLayout), bt.Format(dayLayout)
		hp := hourly[hk]
		hp.Input += b.Input
		hp.Output += b.Output
		hourly[hk] = hp
		dp := daily[dk]
		dp.Input += b.Input
		dp.Output += b.Output
		daily[dk] = dp
	}

	t := Tokens{
		Today: daily[local.Format(dayLayout)],
		Hourly: Hourly{
			Start:  time.Date(y, m, d, h-(hourlyWindow-1), 0, 0, 0, loc).Format(time.RFC3339),
			Input:  make([]int64, hourlyWindow),
			Output: make([]int64, hourlyWindow),
		},
	}
	for i := range hourlyWindow {
		p := hourly[time.Date(y, m, d, h-(hourlyWindow-1)+i, 0, 0, 0, loc).Format(hourLayout)]
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

// windowStart is the earliest instant magpie must cover: the daily window's first day.
func windowStart(now time.Time, loc *time.Location) time.Time {
	return dailyStart(now.In(loc))
}

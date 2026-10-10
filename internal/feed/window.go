package feed

import "time"

// aggregate turns magpie usage into the feed's UTC+8 windows as of now. "Today"
// is the local day's total; hourly is today's 24 local hours from midnight, with
// hours after now zero; daily is the 16-week Monday-first grid, with days after
// today left null.
func aggregate(u Usage, now time.Time, loc *time.Location) Tokens {
	local := now.In(loc)
	y, m, d := local.Date()
	todayMidnight := time.Date(y, m, d, 0, 0, 0, 0, loc)
	hourly := sumByKey(u.Hours, loc, hourLayout)
	daily := sumByKey(u.Days, loc, dayLayout)
	start := dailyStart(local)

	t := Tokens{
		Today: daily[local.Format(dayLayout)],
		Hourly: Hourly{
			Start: todayMidnight.Format(time.RFC3339),
		},
		Daily: Daily{
			Start: start.Format(dayLayout),
		},
	}
	t.Hourly.Input, t.Hourly.Output = fillSeries(hourlyWindow, func(i int) (Pair, bool) {
		return hourly[time.Date(y, m, d, i, 0, 0, 0, loc).Format(hourLayout)], true
	}, func(v int64) int64 { return v })
	t.Daily.Input, t.Daily.Output = fillSeries(dailyDays, func(i int) (Pair, bool) {
		day := time.Date(start.Year(), start.Month(), start.Day()+i, 0, 0, 0, 0, loc)
		if day.After(todayMidnight) {
			return Pair{}, false
		}
		return daily[day.Format(dayLayout)], true
	}, func(v int64) *int64 { return &v })
	return t
}

// sumByKey totals each bucket's tokens under its start instant formatted in loc with layout.
func sumByKey(buckets []Bucket, loc *time.Location, layout string) map[string]Pair {
	sums := make(map[string]Pair)
	for _, b := range buckets {
		k := b.Start.In(loc).Format(layout)
		p := sums[k]
		p.Input += b.Input
		p.Output += b.Output
		sums[k] = p
	}
	return sums
}

// fillSeries builds the input and output series of length n. Index i takes the
// wrapped pair from at when at reports ok; otherwise it stays the zero value.
func fillSeries[T any](n int, at func(i int) (Pair, bool), wrap func(int64) T) (input, output []T) {
	input = make([]T, n)
	output = make([]T, n)
	for i := range n {
		p, ok := at(i)
		if !ok {
			continue
		}
		input[i] = wrap(p.Input)
		output[i] = wrap(p.Output)
	}
	return input, output
}

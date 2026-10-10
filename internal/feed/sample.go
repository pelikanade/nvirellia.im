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

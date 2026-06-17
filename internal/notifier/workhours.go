package notifier

import (
	"strconv"
	"strings"
	"time"
)

// InWorkHours reports whether now falls inside the configured work-hours
// window. An empty Timezone defaults to UTC. A WorkHours with no Days
// configured matches every day.
func InWorkHours(wh WorkHours, now time.Time) bool {
	loc := time.UTC
	if wh.Timezone != "" {
		if l, err := time.LoadLocation(wh.Timezone); err == nil {
			loc = l
		}
	}
	local := now.In(loc)

	if len(wh.Days) > 0 {
		dayMatch := false
		today := local.Format("Mon")
		for _, d := range wh.Days {
			if strings.EqualFold(d, today) {
				dayMatch = true
				break
			}
		}
		if !dayMatch {
			return false
		}
	}

	start, ok1 := parseHHMM(wh.Start)
	end, ok2 := parseHHMM(wh.End)
	if !ok1 || !ok2 {
		return true
	}
	cur := local.Hour()*60 + local.Minute()
	return cur >= start && cur <= end
}

func parseHHMM(s string) (int, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return h*60 + m, true
}

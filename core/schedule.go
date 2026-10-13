package core

import (
	"strconv"
	"strings"
	"time"
)

// Cron supports five numeric fields, lists, ranges and steps. DOM and DOW use conventional OR semantics.
type Cron struct {
	Values   [5]map[int]bool
	Wildcard [5]bool
	Location *time.Location
}

func ParseCron(expression, timezone string) (Cron, error) {
	c := Cron{Location: time.UTC}
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return c, Err("INVALID_ARGUMENT", "cron requires minute hour day month weekday", 400)
	}
	if timezone != "" {
		loc, e := time.LoadLocation(timezone)
		if e != nil {
			return c, Err("INVALID_ARGUMENT", "unknown IANA timezone", 400)
		}
		c.Location = loc
	}
	mins, maxs := []int{0, 0, 1, 1, 0}, []int{59, 23, 31, 12, 7}
	for i, f := range fields {
		c.Values[i] = map[int]bool{}
		c.Wildcard[i] = strings.HasPrefix(f, "*")
		for _, part := range strings.Split(f, ",") {
			base, stepText, has := strings.Cut(part, "/")
			step := 1
			if has {
				n, e := strconv.Atoi(stepText)
				if e != nil || n < 1 || n > maxs[i]+1 {
					return c, Err("INVALID_ARGUMENT", "invalid cron step", 400)
				}
				step = n
			}
			lo, hi := mins[i], maxs[i]
			if base != "*" {
				a, b, ran := strings.Cut(base, "-")
				n, e := strconv.Atoi(a)
				if e != nil {
					return c, Err("INVALID_ARGUMENT", "cron accepts numeric fields", 400)
				}
				lo = n
				hi = n
				if ran {
					n, e = strconv.Atoi(b)
					if e != nil {
						return c, Err("INVALID_ARGUMENT", "invalid cron range", 400)
					}
					hi = n
				} else if has {
					hi = maxs[i]
				}
			}
			if lo < mins[i] || hi > maxs[i] || lo > hi {
				return c, Err("INVALID_ARGUMENT", "cron field outside range", 400)
			}
			for n := lo; n <= hi; n += step {
				if i == 4 && n == 7 {
					c.Values[i][0] = true
				} else {
					c.Values[i][n] = true
				}
			}
		}
	}
	return c, nil
}
func (c Cron) Matches(t time.Time) bool {
	t = t.In(c.Location)
	if !c.Values[0][t.Minute()] || !c.Values[1][t.Hour()] || !c.Values[3][int(t.Month())] {
		return false
	}
	dom, dow := c.Values[2][t.Day()], c.Values[4][int(t.Weekday())]
	if !c.Wildcard[2] && !c.Wildcard[4] {
		return dom || dow
	}
	return dom && dow
}

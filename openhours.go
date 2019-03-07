package openhours

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

var weekDays = map[string]int{"su": 0, "mo": 1, "tu": 2, "we": 3, "th": 4, "fr": 5, "sa": 6}

// OpenHours ...
type OpenHours []time.Time

func newDate(day, hour, min, sec, nsec int, loc *time.Location) time.Time {
	return time.Date(2017, 1, day, hour, min, sec, nsec, loc)
}

func newDateFromTime(t time.Time) time.Time {
	return newDate(int(t.Weekday()), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// Match returns true if the time t is in the open hours
func (o OpenHours) Match(t time.Time) bool {
	t = newDateFromTime(t)
	i := o.matchIndex(t)
	return i%2 == 1
}

func (o OpenHours) matchIndex(t time.Time) int {
	i := 0
	for ; i < len(o); i++ {
		if o[i].After(t) {
			break
		}
	}
	return i
}

// NextDur returns true if t is in the open hours and the duration until it closes
// else it returns false if t is in the closed hours and the duration until it opens
func (o OpenHours) NextDur(t time.Time) (bool, time.Duration) {
	x := newDateFromTime(t)
	i := o.matchIndex(x)
	b := i%2 == 1
	if i == len(o) {
		i = 0
	}
	oi := o[i]
	if x.After(oi) {
		oi = oi.Add(time.Hour * 24 * 7) // add a week
	}
	diff := oi.Sub(x)
	_, offset := t.Zone()
	_, newOffset := t.Add(diff).Zone()
	diff += time.Duration(time.Duration(offset-newOffset) * time.Second)
	return b, diff
}

// NextDate uses nextDur to gives the date of interest
func (o OpenHours) NextDate(t time.Time) (bool, time.Time) {
	b, dur := o.NextDur(t)
	return b, t.Add(dur)
}

func cleanStr(str string) string {
	clean := strings.TrimSpace(str)
	clean = strings.Join(strings.Fields(clean), " ")
	clean = strings.ToLower(clean)
	clean = strings.Replace(clean, " ,", ",", -1)
	clean = strings.Replace(clean, ", ", ",", -1)
	return clean
}

func simplifyDays(str string) []int {
	simple := []int{}
	days := map[int]struct{}{}
	for _, str := range strings.Split(str, ",") {
		switch len(str) {
		case 2: // "mo"
			if v, exist := weekDays[str]; exist {
				days[v] = struct{}{}
			}
			continue
		case 5: // "tu-fr"
			strs := strings.Split(str, "-")
			from, exist := weekDays[strs[0]]
			if !exist {
				continue
			}
			to, exist := weekDays[strs[1]]
			if !exist {
				continue
			}
			if to < from { // circular lookup
				to += 7
			}
			for i := from; i <= to; i++ {
				days[i%7] = struct{}{}
			}
			continue
		}
	}
	for i := range days {
		simple = append(simple, i)
	}
	sort.Ints(simple)
	return simple
}

func simplifyHour(str string) (int, int) {
	hour, min := 0, 0
	strs := strings.Split(str, ":")
	if len(strs) != 2 {
		return 0, 0
	}
	hour, _ = strconv.Atoi(strs[0])
	min, _ = strconv.Atoi(strs[1])
	if hour > 24 || hour < 0 || min > 60 || min < 0 || (hour == 24 && min > 0) {
		return 0, 0
	}
	return hour, min
}

// New returns a new instance of an openhours
func New(str string, loc *time.Location) OpenHours {
	if loc == nil {
		loc = time.UTC
	}
	o := OpenHours{}
	if len(str) > 0 && str[len(str)-1] == ';' {
		str = str[:len(str)-1]
	}
	if str == "" {
		str = "su-sa 00:00-24:00"
	}
	for _, str := range strings.Split(cleanStr(str), ";") {
		strs := strings.Fields(str)
		days := simplifyDays(strs[0])
		for _, str := range strings.Split(strs[1], ",") {
			times := strings.Split(str, "-")
			hourFrom, minFrom := simplifyHour(times[0])
			hourTo, minTo := simplifyHour(times[1])
			for _, day := range days {
				o = append(o, newDate(day, hourFrom, minFrom, 0, 0, loc), newDate(day, hourTo, minTo, 0, 0, loc))
			}
		}
	}
	sort.Slice(o, func(i, j int) bool {
		return o[i].Before(o[j])
	})
	newT := []time.Time{o[0]}
	for i := 1; i+1 < len(o); i += 2 {
		if o[i].Equal(o[i+1]) {
			continue
		}
		newT = append(newT, o[i], o[i+1])
	}
	newT = append(newT, o[len(o)-1])
	return newT
}

package openhours

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	Monday int = iota + 1
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
	Sunday
)

var (
	weekDays = map[string]int{"mo": Monday, "tu": Tuesday, "we": Wednesday, "th": Thursday, "fr": Friday, "sa": Saturday, "su": Sunday}

	// Errors
	ErrInvalidFormat error = errors.New("invalid format")
)

// OpenHours ...
type OpenHours []time.Time

func newDate(day, hour, min, sec, nsec int, loc *time.Location) time.Time {
	return time.Date(2018, 1, day, hour, min, sec, nsec, loc)
}

func newDateFromTime(t time.Time) time.Time {
	return newDate(t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// Match returns true if the time t is in the open hours
func (o OpenHours) Match(t time.Time) bool {
	t = newDateFromTime(t)
	i := o.matchIndex(t)
	return i%2 == 1
}

// matchIndex returns the index of the next open hour
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
	current := newDateFromTime(t)
	i := o.matchIndex(current)
	isOpen := i%2 == 1 // uneven -> next time is a closing time
	if i == len(o) {   // end of week, wrap around
		i = 0
	}
	next := o[i]
	if current.After(next) { // we wrapped, set days to end of week
		next = next.AddDate(0, 0, 7)
	}
	return isOpen, tzDiff(next, current, t)
}

// tzDiff calculate diff between a and b and add it to t, taking in account eventual tz changes
func tzDiff(a, b, t time.Time) time.Duration {
	diff := a.Sub(b)
	_, offset := t.Zone()
	_, newOffset := t.Add(diff).Zone()
	return diff + time.Duration(offset-newOffset)*time.Second
}

// When returns the date where the duration can be done in one go during open hours
func (o OpenHours) When(t time.Time, d time.Duration) *time.Time {
	x := newDateFromTime(t)
	i := o.matchIndex(x)
	var found *time.Time
	if i%2 == 1 {
		newO := x.Add(d)
		// log.Println(x, newO, o[i], newO.Before(o[i]) || newO.Equal(o[i]), i, o)
		if newO.Before(o[i]) || newO.Equal(o[i]) {
			found = &x
		} else {
			i += 2
		}
	} else {
		i++
	}
	for max := i + len(o); i < max && found == nil; i += 2 {
		newI := i % len(o)
		newO := o[newI-1].Add(d)
		if newO.Before(o[newI]) || newO.Equal(o[newI]) {
			found = &o[newI-1]
		}
	}
	if found == nil {
		return found
	}
	if x.After(*found) {
		z := found.AddDate(0, 0, 7)
		found = &z
	}
	f := t.Add(tzDiff(*found, x, t))
	return &f
}

// NextDate uses nextDur to gives the date of interest
func (o OpenHours) NextDate(t time.Time) (bool, time.Time) {
	b, dur := o.NextDur(t)
	return b, t.Add(dur)
}

func (o OpenHours) Add(from, to time.Time) OpenHours {
	o = append(o, newDateFromTime(from), newDateFromTime(to))
	o = merge(o)
	return o
}

func (o OpenHours) String() []string {
	str := []string{}
	if len(o) == 0 {
		return str
	}
	for i := 1; i <= len(o)-1; i += 2 {
		str = append(str, fmt.Sprintf("%s %s - %s", o[i-1].Weekday(), o[i-1].Format("15:04"), o[i].Format("15:04")))
	}
	return str
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
				switch i % 7 {
				case 0:
					days[7] = struct{}{}
				default:
					days[i%7] = struct{}{}
				}
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

func simplifyTime(str string) (int, int, int) {
	hour, min := 0, 0
	strs := strings.Split(str, ":")
	if len(strs) < 2 || len(strs) > 3 {
		return 0, 0, 0
	}
	hour, _ = strconv.Atoi(strs[0])
	min, _ = strconv.Atoi(strs[1])
	var sec int
	if len(strs) == 3 {
		sec, _ = strconv.Atoi(strs[2])
	}
	if hour > 24 {
		hour = hour % 24
	}
	if hour > 24 || hour < 0 || min > 59 || min < 0 || sec > 59 || sec < 0 || (hour == 24 && min > 0 || hour == 24 && sec > 0) {
		return 0, 0, 0
	}
	return hour, min, sec
}

func new(str string, loc *time.Location) (OpenHours, error) {
	if loc == nil {
		loc = time.UTC
	}
	o := []time.Time{}
	if len(str) > 0 && str[len(str)-1] == ';' {
		str = str[:len(str)-1]
	}
	if str == "" {
		str = "su-sa 00:00-24:00"
	}
	for _, str := range strings.Split(cleanStr(str), ";") {
		strs := strings.Fields(str)
		if len(strs) < 2 {
			return nil, ErrInvalidFormat
		}
		days := simplifyDays(strs[0])
		for _, str := range strings.Split(strs[1], ",") {
			times := strings.Split(str, "-")
			if len(times) != 2 {
				return nil, ErrInvalidFormat
			}
			hourFrom, minFrom, secFrom := simplifyTime(times[0])
			hourTo, minTo, secTo := simplifyTime(times[1])
			for _, day := range days {
				fromDate := newDate(day, hourFrom, minFrom, secFrom, 0, loc)
				if hourFrom > hourTo { // closing after midnight
					day++
				}
				o = append(o, fromDate, newDate(day, hourTo, minTo, secTo, 0, loc))
			}
		}
	}
	return o, nil
}

func merge4(o ...time.Time) (bool, []time.Time) {
	for i := 0; i < len(o)-1; i++ {
		if o[i].After(o[i+1]) || o[i].Equal(o[i+1]) {
			sort.Slice(o, func(i, j int) bool {
				return o[i].Before(o[j])
			})
			return true, []time.Time{o[0], o[len(o)-1]}
		}
	}
	return false, nil
}

func merge(o []time.Time) []time.Time {
	sort.SliceStable(o, func(i, j int) bool {
		if o[i].Day() == o[j].Day() {
			return o[i].Hour() < o[j].Hour()
		}
		if o[i].Year() == o[j].Year() {
			return o[i].Day() < o[j].Day()
		}
		return o[i].Year() < o[j].Year()
	})
	for i := 0; i < len(o); i += 2 {
		for j := i + 2; j < len(o); j += 2 {
			perform, res := merge4(o[i], o[i+1], o[j], o[j+1])
			if !perform {
				continue
			}
			o[i], o[i+1] = res[0], res[1]
			o = append(o[:j], o[j+2:]...)
			i -= 2
			break
		}
	}
	return o
}

// New returns a new instance of an openhours.
// If loc is nil, UTC is used.
func New(str string, loc *time.Location) (OpenHours, error) {
	o, err := new(str, loc)
	return merge(o), err
}

// NewMust returns a new instance of an openhours or panics on error
// If loc is nil, UTC is used.
func NewMust(str string, loc *time.Location) OpenHours {
	o, err := new(str, loc)
	if err != nil {
		panic(err)
	}
	return merge(o)
}

// NewLocal returns a new instance of an openhours with local timezone
func NewLocal(str string) (OpenHours, error) {
	return New(str, time.Local)
}

// NewUTC returns a new instance of an openhours with UTC timezone
func NewUTC(str string) (OpenHours, error) {
	return New(str, time.UTC)
}

package openhours

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"
)

const minutesPerWeek = 10080

var (
	internMu        sync.RWMutex
	internPool      = make(map[string]*OpeningHours)
	zeroDuration    time.Duration
	weekdayToDayIdx = [7]int{6, 0, 1, 2, 3, 4, 5} // Sunday (0) -> 6, Monday (1) -> 0, ...

	emptyOH      = &OpeningHours{expression: "", windows: nil}
	alwaysOpenOH = &OpeningHours{expression: "24/7", windows: []TimeWindow{{Start: 0, End: minutesPerWeek}}}
)

// TimeWindow represents a minute interval [Start, End) within a week (0 to 10080 minutes).
// Monday 00:00 is minute 0, and Sunday 24:00 is minute 10080.
type TimeWindow struct {
	Start int
	End   int
}

// OpeningHours is an immutable, interval-math based evaluator for OSM opening_hours expressions.
// It supports overnight shifts, off/closed overrides, 24/7, open-ended ranges, and multi-interval rules.
type OpeningHours struct {
	expression string
	windows    []TimeWindow
}

// Parse parses an OSM opening_hours expression string and returns an *OpeningHours instance.
// If the expression is invalid or empty, it returns a safe instance with empty schedule (IsOpen = false).
// Identical expressions are interned to conserve memory.
func Parse(expression string) *OpeningHours {
	if expression == "" {
		return emptyOH
	}
	if expression == "24/7" {
		return alwaysOpenOH
	}

	internMu.RLock()
	cached, ok := internPool[expression]
	internMu.RUnlock()
	if ok {
		return cached
	}

	trimmed := strings.TrimSpace(expression)

	internMu.Lock()
	defer internMu.Unlock()
	if cached, ok := internPool[expression]; ok {
		return cached
	}

	var oh *OpeningHours
	switch {
	case trimmed == "":
		oh = emptyOH
	case trimmed == "24/7":
		oh = alwaysOpenOH
	default:
		var rulesBuf [8]openingRule
		rules := rulesBuf[:0]

		remaining := trimmed
		for len(remaining) > 0 {
			var part string
			if idx := strings.IndexByte(remaining, ';'); idx >= 0 {
				part = remaining[:idx]
				remaining = remaining[idx+1:]
			} else {
				part = remaining
				remaining = ""
			}
			part = strings.TrimSpace(part)
			if part != "" {
				rule := parseOpeningRule(part)
				if rule.DayMask > 0 && (rule.IsAllDay || rule.hasRanges()) {
					rules = append(rules, rule)
				}
			}
		}
		windows := bakeRules(rules)
		oh = &OpeningHours{expression: expression, windows: windows}
	}

	internPool[expression] = oh
	return oh
}

// Raw returns the raw expression string.
func (oh *OpeningHours) Raw() string {
	if oh == nil {
		return ""
	}
	return oh.expression
}

// String returns the raw expression string.
func (oh *OpeningHours) String() string {
	if oh == nil {
		return ""
	}
	return oh.expression
}

// Windows returns a copy of the disjoint time windows (in week minutes) that comprise the schedule.
func (oh *OpeningHours) Windows() []TimeWindow {
	if oh == nil || len(oh.windows) == 0 {
		return nil
	}
	return slices.Clone(oh.windows)
}

// IsOpen returns true if the specified time is within opening hours.
func (oh *OpeningHours) IsOpen(t time.Time) bool {
	if oh == nil || len(oh.windows) == 0 {
		return false
	}
	var weekMin int
	if t.Location() == time.UTC {
		unixSec := t.Unix()
		weekMin = int((unixSec/60 + 4320) % minutesPerWeek)
		if weekMin < 0 {
			weekMin += minutesPerWeek
		}
	} else {
		hour, min, _ := t.Clock()
		day := weekdayToDayIdx[t.Weekday()]
		weekMin = day*1440 + hour*60 + min
	}
	return oh.findWindowIndex(weekMin) != -1
}

// Match returns true if the time t is within opening hours.
func (oh *OpeningHours) Match(t time.Time) bool {
	return oh.IsOpen(t)
}

// GetCurrentShiftEnd returns the end time of the current open shift if open at t,
// or nil if currently closed, or nil if open 24/7 (infinite open).
func (oh *OpeningHours) GetCurrentShiftEnd(t time.Time) *time.Time {
	if oh == nil || len(oh.windows) == 0 {
		return nil
	}
	if len(oh.windows) == 1 && oh.windows[0].Start == 0 && oh.windows[0].End == minutesPerWeek {
		return nil
	}

	min, subMinute := getWeekMinute(t)
	idx := oh.findWindowIndex(min)
	if idx == -1 {
		return nil
	}

	w := oh.windows[idx]
	diffMin := w.End - min
	if idx == len(oh.windows)-1 && w.End == minutesPerWeek && oh.windows[0].Start == 0 {
		diffMin = (minutesPerWeek - min) + oh.windows[0].End
	}

	res := t.Add(time.Duration(diffMin)*time.Minute - subMinute)
	return &res
}

// GetTimeToOpen returns the duration until the next opening window starting from `from`.
// If currently open, returns 0. If never opens, returns nil.
func (oh *OpeningHours) GetTimeToOpen(from time.Time) *time.Duration {
	d, ok := oh.getTimeToOpen(from)
	if !ok {
		return nil
	}
	if d == 0 {
		return &zeroDuration
	}
	return &d
}

func (oh *OpeningHours) getTimeToOpen(from time.Time) (time.Duration, bool) {
	if oh == nil || len(oh.windows) == 0 {
		return 0, false
	}
	if len(oh.windows) == 1 && oh.windows[0].Start == 0 && oh.windows[0].End == minutesPerWeek {
		return 0, true
	}

	t, subMinute := getWeekMinute(from)
	if oh.findWindowIndex(t) != -1 {
		return 0, true
	}

	idx := oh.findFirstWindowStartingAtOrAfter(t)

	if idx < len(oh.windows) {
		d := time.Duration(oh.windows[idx].Start-t)*time.Minute - subMinute
		return d, true
	}

	d := time.Duration((minutesPerWeek-t)+oh.windows[0].Start)*time.Minute - subMinute
	return d, true
}

// GetTimeToOpenForDuration returns the wait duration from `from` until an opening window
// that is at least `duration` long begins (or 0 if currently open and remaining continuous slot >= duration).
// Returns nil if no single opening window in the schedule is long enough.
func (oh *OpeningHours) GetTimeToOpenForDuration(from time.Time, duration time.Duration) *time.Duration {
	d, ok := oh.getTimeToOpenForDuration(from, duration)
	if !ok {
		return nil
	}
	if d == 0 {
		return &zeroDuration
	}
	return &d
}

func (oh *OpeningHours) getTimeToOpenForDuration(from time.Time, duration time.Duration) (time.Duration, bool) {
	if oh == nil || len(oh.windows) == 0 {
		return 0, false
	}
	if duration <= 0 {
		return 0, true
	}
	if duration > time.Duration(minutesPerWeek)*time.Minute {
		return 0, false
	}
	if len(oh.windows) == 1 && oh.windows[0].Start == 0 && oh.windows[0].End == minutesPerWeek {
		return 0, true
	}

	t, subMinute := getWeekMinute(from)
	startIdx := oh.findFirstWindowStartingAtOrAfter(t)

	// Check windows in current week starting from t
	for i := startIdx; i < len(oh.windows); i++ {
		w := oh.windows[i]
		effectiveEnd := w.End
		if i == len(oh.windows)-1 && w.End == minutesPerWeek && oh.windows[0].Start == 0 {
			effectiveEnd = minutesPerWeek + oh.windows[0].End
		}

		if t >= w.Start {
			remDur := time.Duration(effectiveEnd-t)*time.Minute - subMinute
			if remDur >= duration {
				return 0, true
			}
		} else {
			winDur := time.Duration(effectiveEnd-w.Start) * time.Minute
			if winDur >= duration {
				d := time.Duration(w.Start-t)*time.Minute - subMinute
				return d, true
			}
		}
	}

	// Wrap around to next week
	for i := 0; i < len(oh.windows); i++ {
		w := oh.windows[i]
		effectiveEnd := w.End
		if i == len(oh.windows)-1 && w.End == minutesPerWeek && oh.windows[0].Start == 0 {
			effectiveEnd = minutesPerWeek + oh.windows[0].End
		}

		winDur := time.Duration(effectiveEnd-w.Start) * time.Minute
		if winDur >= duration {
			d := time.Duration((minutesPerWeek-t)+w.Start)*time.Minute - subMinute
			return d, true
		}
	}

	return 0, false
}

// When returns the time `from` + wait duration when `duration` can be continuously serviced during open hours,
// or nil if it never fits.
func (oh *OpeningHours) When(from time.Time, duration time.Duration) *time.Time {
	d, ok := oh.getTimeToOpenForDuration(from, duration)
	if !ok {
		return nil
	}
	if d == 0 {
		return &from
	}
	res := from.Add(d)
	return &res
}

// NextDur returns whether currently open, and the duration until state changes (shift ends or opens).
func (oh *OpeningHours) NextDur(t time.Time) (bool, time.Duration) {
	if oh == nil || len(oh.windows) == 0 {
		return false, 0
	}
	min, subMinute := getWeekMinute(t)
	idx := oh.findWindowIndex(min)
	if idx != -1 {
		// Currently open
		if len(oh.windows) == 1 && oh.windows[0].Start == 0 && oh.windows[0].End == minutesPerWeek {
			return true, time.Duration(minutesPerWeek) * time.Minute
		}
		w := oh.windows[idx]
		diffMin := w.End - min
		if idx == len(oh.windows)-1 && w.End == minutesPerWeek && oh.windows[0].Start == 0 {
			diffMin = (minutesPerWeek - min) + oh.windows[0].End
		}
		dur := time.Duration(diffMin)*time.Minute - subMinute
		return true, dur
	}

	// Currently closed
	if len(oh.windows) == 1 && oh.windows[0].Start == 0 && oh.windows[0].End == minutesPerWeek {
		return false, 0
	}
	openIdx := oh.findFirstWindowStartingAtOrAfter(min)
	if openIdx < len(oh.windows) {
		dur := time.Duration(oh.windows[openIdx].Start-min)*time.Minute - subMinute
		return false, dur
	}
	dur := time.Duration((minutesPerWeek-min)+oh.windows[0].Start)*time.Minute - subMinute
	return false, dur
}

// NextDate returns whether currently open, and the time when the status changes.
func (oh *OpeningHours) NextDate(t time.Time) (bool, time.Time) {
	isOpen, dur := oh.NextDur(t)
	return isOpen, t.Add(dur)
}

func (oh *OpeningHours) findWindowIndex(t int) int {
	windows := oh.windows
	n := len(windows)
	if n == 0 {
		return -1
	}
	_ = windows[n-1] // BCE
	low := 0
	high := n - 1
	for low <= high {
		mid := int(uint(low+high) >> 1)
		w := &windows[mid]
		if t < w.Start {
			high = mid - 1
		} else if t < w.End {
			return mid
		} else {
			low = mid + 1
		}
	}
	return -1
}

func (oh *OpeningHours) findFirstWindowStartingAtOrAfter(t int) int {
	windows := oh.windows
	n := len(windows)
	if n == 0 {
		return 0
	}
	_ = windows[n-1] // BCE
	low := 0
	high := n - 1
	result := n
	for low <= high {
		mid := int(uint(low+high) >> 1)
		if windows[mid].End > t {
			result = mid
			high = mid - 1
		} else {
			low = mid + 1
		}
	}
	return result
}

// MarshalJSON serializes OpeningHours as a JSON string with its expression.
func (oh *OpeningHours) MarshalJSON() ([]byte, error) {
	if oh == nil {
		return []byte("null"), nil
	}
	return json.Marshal(oh.expression)
}

// UnmarshalJSON deserializes OpeningHours from a JSON string.
func (oh *OpeningHours) UnmarshalJSON(data []byte) error {
	n := len(data)
	if n >= 2 && data[0] == '"' && data[n-1] == '"' && !slices.Contains(data, '\\') {
		*oh = *Parse(string(data[1 : n-1]))
		return nil
	}
	var expr string
	if err := json.Unmarshal(data, &expr); err != nil {
		return err
	}
	parsed := Parse(expr)
	*oh = *parsed
	return nil
}

func getWeekMinute(dt time.Time) (int, time.Duration) {
	if dt.Location() == time.UTC {
		unixSec := dt.Unix()
		weekMin := int((unixSec/60 + 4320) % minutesPerWeek)
		if weekMin < 0 {
			weekMin += minutesPerWeek
		}
		subMinute := time.Duration(unixSec%60)*time.Second + time.Duration(dt.Nanosecond())*time.Nanosecond
		return weekMin, subMinute
	}
	hour, min, sec := dt.Clock()
	day := weekdayToDayIdx[dt.Weekday()]
	subMinute := time.Duration(sec)*time.Second + time.Duration(dt.Nanosecond())*time.Nanosecond
	return day*1440 + hour*60 + min, subMinute
}

type timeRange struct {
	StartMin  int16
	EndMin    int16
	DayOffset int8
}

type openingRule struct {
	DayMask     uint8
	IsOff       bool
	IsAllDay    bool
	NumRanges   uint8
	TimeRanges  [4]timeRange
	ExtraRanges []timeRange
}

func (r *openingRule) addRange(tr timeRange) {
	if r.NumRanges < 4 {
		r.TimeRanges[r.NumRanges] = tr
		r.NumRanges++
	} else {
		r.ExtraRanges = append(r.ExtraRanges, tr)
	}
}

func (r *openingRule) hasRanges() bool {
	return r.NumRanges > 0 || len(r.ExtraRanges) > 0
}

func hasSuffixFold(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	s = s[len(s)-len(suffix):]
	for i := 0; i < len(suffix); i++ {
		c1 := s[i]
		c2 := suffix[i]
		if c1 != c2 {
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 'a' - 'A'
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 'a' - 'A'
			}
			if c1 != c2 {
				return false
			}
		}
	}
	return true
}

func equalFoldASCII(s, target string) bool {
	if len(s) != len(target) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c1 := s[i]
		c2 := target[i]
		if c1 != c2 {
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 'a' - 'A'
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 'a' - 'A'
			}
			if c1 != c2 {
				return false
			}
		}
	}
	return true
}

func parseOpeningRule(ruleString string) openingRule {
	var rule openingRule

	if hasSuffixFold(ruleString, " off") {
		rule.IsOff = true
		ruleString = strings.TrimSpace(ruleString[:len(ruleString)-4])
	} else if hasSuffixFold(ruleString, " closed") {
		rule.IsOff = true
		ruleString = strings.TrimSpace(ruleString[:len(ruleString)-7])
	} else if hasSuffixFold(ruleString, " open") {
		ruleString = strings.TrimSpace(ruleString[:len(ruleString)-5])
	} else if equalFoldASCII(ruleString, "off") || equalFoldASCII(ruleString, "closed") {
		rule.IsOff = true
		rule.DayMask = 0x7F
		rule.IsAllDay = true
		return rule
	} else if equalFoldASCII(ruleString, "open") || ruleString == "24/7" {
		rule.DayMask = 0x7F
		rule.IsAllDay = true
		return rule
	}

	ruleString = strings.TrimSpace(ruleString)
	if ruleString == "" {
		return rule
	}

	// Find the first ASCII digit which begins the time specification
	digitIdx := -1
	for i := 0; i < len(ruleString); i++ {
		if ruleString[i] >= '0' && ruleString[i] <= '9' {
			digitIdx = i
			break
		}
	}

	var dayPart, timePart string
	if digitIdx >= 0 {
		dayPart = strings.TrimSpace(ruleString[:digitIdx])
		timePart = strings.TrimSpace(ruleString[digitIdx:])
	} else {
		dayPart = ruleString
		timePart = ""
	}

	if dayPart != "" {
		dayMask := parseDayMask(dayPart)
		if dayMask == 0 {
			return rule
		}
		rule.DayMask = uint8(dayMask)
	} else {
		rule.DayMask = 0x7F // All 7 days
	}

	if timePart != "" {
		parseTimesToRule(timePart, &rule)
		rule.IsAllDay = false
	} else {
		rule.IsAllDay = true
	}

	return rule
}

func parseDayMask(dayPart string) int {
	mask := 0
	remaining := dayPart
	for len(remaining) > 0 {
		var group string
		if idx := strings.IndexByte(remaining, ','); idx >= 0 {
			group = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			group = remaining
			remaining = ""
		}
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if dashIdx := strings.IndexByte(group, '-'); dashIdx >= 0 {
			rangePart1 := strings.TrimSpace(group[:dashIdx])
			rangePart2 := strings.TrimSpace(group[dashIdx+1:])
			start := dayToIndex(rangePart1)
			end := dayToIndex(rangePart2)
			if start != -1 && end != -1 {
				for curr := start; ; curr = (curr + 1) % 7 {
					mask |= (1 << curr)
					if curr == end {
						break
					}
				}
			} else {
				return 0
			}
		} else {
			idx := dayToIndex(group)
			if idx != -1 {
				mask |= (1 << idx)
			} else {
				return 0
			}
		}
	}
	return mask
}

func dayToIndex(s string) int {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return -1
	}
	c0 := s[0] | 0x20
	c1 := s[1] | 0x20

	switch {
	case c0 == 'm' && c1 == 'o':
		if len(s) == 2 || equalFoldASCII(s, "mon") || equalFoldASCII(s, "monday") {
			return 0
		}
	case c0 == 't' && c1 == 'u':
		if len(s) == 2 || equalFoldASCII(s, "tue") || equalFoldASCII(s, "tues") || equalFoldASCII(s, "tuesday") {
			return 1
		}
	case c0 == 'w' && c1 == 'e':
		if len(s) == 2 || equalFoldASCII(s, "wed") || equalFoldASCII(s, "wednesday") {
			return 2
		}
	case c0 == 't' && c1 == 'h':
		if len(s) == 2 || equalFoldASCII(s, "thu") || equalFoldASCII(s, "thur") || equalFoldASCII(s, "thurs") || equalFoldASCII(s, "thursday") {
			return 3
		}
	case c0 == 'f' && c1 == 'r':
		if len(s) == 2 || equalFoldASCII(s, "fri") || equalFoldASCII(s, "friday") {
			return 4
		}
	case c0 == 's' && c1 == 'a':
		if len(s) == 2 || equalFoldASCII(s, "sat") || equalFoldASCII(s, "saturday") {
			return 5
		}
	case c0 == 's' && c1 == 'u':
		if len(s) == 2 || equalFoldASCII(s, "sun") || equalFoldASCII(s, "sunday") {
			return 6
		}
	}
	return -1
}

func parseTimesToRule(timePart string, rule *openingRule) {
	remaining := timePart
	for len(remaining) > 0 {
		var group string
		if idx := strings.IndexByte(remaining, ','); idx >= 0 {
			group = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			group = remaining
			remaining = ""
		}
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if group == "24/7" || group == "00:00-24:00" || group == "00:00-00:00" {
			rule.addRange(timeRange{StartMin: 0, EndMin: 1440, DayOffset: 0})
		} else if dashIdx := strings.IndexByte(group, '-'); dashIdx >= 0 {
			part1 := group[:dashIdx]
			part2 := group[dashIdx+1:]
			s, ok1 := tryParseTimeMin(part1)
			e, ok2 := tryParseTimeMin(part2)
			if ok1 && ok2 {
				if s == 0 && e == 0 {
					rule.addRange(timeRange{StartMin: 0, EndMin: 1440, DayOffset: 0})
				} else if s < e {
					rule.addRange(timeRange{StartMin: int16(s), EndMin: int16(e), DayOffset: 0})
				} else if s > e {
					rule.addRange(timeRange{StartMin: int16(s), EndMin: 1440, DayOffset: 0})
					rule.addRange(timeRange{StartMin: 0, EndMin: int16(e), DayOffset: 1})
				}
			}
		} else if strings.HasSuffix(group, "+") {
			sOnly, ok := tryParseTimeMin(group[:len(group)-1])
			if ok {
				rule.addRange(timeRange{StartMin: int16(sOnly), EndMin: 1440, DayOffset: 0})
			}
		}
	}
}

func tryParseTimeMin(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "24:00" {
		return 1440, true
	}
	n := len(s)
	// Fast path for standard "HH:MM" (5 bytes)
	if n == 5 && s[2] == ':' {
		h0, h1, m0, m1 := s[0]-'0', s[1]-'0', s[3]-'0', s[4]-'0'
		if h0 < 10 && h1 < 10 && m0 < 10 && m1 < 10 {
			h := int(h0*10 + h1)
			m := int(m0*10 + m1)
			if h < 24 && m < 60 {
				return h*60 + m, true
			}
		}
		return 0, false
	}
	// Fast path for standard "H:MM" (4 bytes)
	if n == 4 && s[1] == ':' {
		h0, m0, m1 := s[0]-'0', s[2]-'0', s[3]-'0'
		if h0 < 10 && m0 < 10 && m1 < 10 {
			m := int(m0*10 + m1)
			if m < 60 {
				return int(h0)*60 + m, true
			}
		}
		return 0, false
	}

	colonIdx := strings.IndexByte(s, ':')
	if colonIdx < 1 || colonIdx >= len(s)-1 {
		return 0, false
	}
	h, ok1 := parseTwoDigits(s[:colonIdx])
	m, ok2 := parseTwoDigits(s[colonIdx+1:])
	if ok1 && ok2 && h >= 0 && h < 24 && m >= 0 && m < 60 {
		return h*60 + m, true
	}
	return 0, false
}

func parseTwoDigits(s string) (int, bool) {
	s = strings.TrimSpace(s)
	switch len(s) {
	case 1:
		if s[0] >= '0' && s[0] <= '9' {
			return int(s[0] - '0'), true
		}
	case 2:
		c0, c1 := s[0], s[1]
		if c0 >= '0' && c0 <= '9' && c1 >= '0' && c1 <= '9' {
			return int(c0-'0')*10 + int(c1-'0'), true
		}
	}
	return 0, false
}

func bakeRules(rules []openingRule) []TimeWindow {
	var openBuf [32]TimeWindow
	openIntervals := openBuf[:0]

	var ruleBuf [16]TimeWindow

	for _, rule := range rules {
		ruleWindows := ruleBuf[:0]
		for day := 0; day < 7; day++ {
			if (rule.DayMask & (1 << day)) == 0 {
				continue
			}

			if rule.IsAllDay {
				dayOffset := day * 1440
				ruleWindows = append(ruleWindows, TimeWindow{Start: dayOffset, End: dayOffset + 1440})
			} else {
				n := int(rule.NumRanges)
				if n > 4 {
					n = 4
				}
				for k := 0; k < n; k++ {
					tr := rule.TimeRanges[k]
					targetDay := (day + int(tr.DayOffset)) % 7
					dayOffset := targetDay * 1440
					ruleWindows = append(ruleWindows, TimeWindow{
						Start: dayOffset + int(tr.StartMin),
						End:   dayOffset + int(tr.EndMin),
					})
				}
				for _, tr := range rule.ExtraRanges {
					targetDay := (day + int(tr.DayOffset)) % 7
					dayOffset := targetDay * 1440
					ruleWindows = append(ruleWindows, TimeWindow{
						Start: dayOffset + int(tr.StartMin),
						End:   dayOffset + int(tr.EndMin),
					})
				}
			}
		}

		if !rule.IsOff {
			openIntervals = append(openIntervals, ruleWindows...)
			openIntervals = mergeWindowsInPlace(openIntervals)
		} else {
			openIntervals = subtractWindowsInPlace(openIntervals, ruleWindows)
		}
	}

	sortWindows(openIntervals)
	if len(openIntervals) == 0 {
		return nil
	}
	res := make([]TimeWindow, len(openIntervals))
	copy(res, openIntervals)
	return res
}

func sortWindows(intervals []TimeWindow) {
	slices.SortFunc(intervals, func(a, b TimeWindow) int {
		return a.Start - b.Start
	})
}

func mergeWindowsInPlace(intervals []TimeWindow) []TimeWindow {
	if len(intervals) <= 1 {
		return intervals
	}
	sortWindows(intervals)

	wIdx := 0
	for i := 1; i < len(intervals); i++ {
		curr := intervals[i]
		if curr.Start <= intervals[wIdx].End {
			if curr.End > intervals[wIdx].End {
				intervals[wIdx].End = curr.End
			}
		} else {
			wIdx++
			intervals[wIdx] = curr
		}
	}
	return intervals[:wIdx+1]
}

func subtractWindowsInPlace(source []TimeWindow, subtrahends []TimeWindow) []TimeWindow {
	subs := mergeWindowsInPlace(subtrahends)

	var nextBuf [32]TimeWindow
	for _, sub := range subs {
		nextResult := nextBuf[:0]
		for _, s := range source {
			if sub.Start >= s.End || sub.End <= s.Start {
				nextResult = append(nextResult, s)
			} else {
				if sub.Start > s.Start {
					nextResult = append(nextResult, TimeWindow{Start: s.Start, End: sub.Start})
				}
				if sub.End < s.End {
					nextResult = append(nextResult, TimeWindow{Start: sub.End, End: s.End})
				}
			}
		}
		source = append(source[:0], nextResult...)
	}
	return source
}



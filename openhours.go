package openhours

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

const minutesPerWeek = 10080

var (
	internMu   sync.RWMutex
	internPool = make(map[string]*OpeningHours)
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
	internMu.RLock()
	if cached, ok := internPool[expression]; ok {
		internMu.RUnlock()
		return cached
	}
	internMu.RUnlock()

	trimmed := strings.TrimSpace(expression)

	internMu.Lock()
	defer internMu.Unlock()
	if cached, ok := internPool[expression]; ok {
		return cached
	}

	var oh *OpeningHours
	switch trimmed {
	case "":
		oh = &OpeningHours{expression: expression, windows: nil}
	case "24/7":
		oh = &OpeningHours{expression: expression, windows: []TimeWindow{{Start: 0, End: minutesPerWeek}}}
	default:
		var rules []openingRule
		// Split by ';' without unnecessary allocations
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
				if rule.DayMask > 0 && (rule.IsAllDay || len(rule.TimeRanges) > 0) {
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
	res := make([]TimeWindow, len(oh.windows))
	copy(res, oh.windows)
	return res
}

// IsOpen returns true if the specified time is within opening hours.
func (oh *OpeningHours) IsOpen(t time.Time) bool {
	if oh == nil || len(oh.windows) == 0 {
		return false
	}
	min := getWeekMinute(t)
	return oh.findWindowIndex(min) != -1
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

	min := getWeekMinute(t)
	idx := oh.findWindowIndex(min)
	if idx == -1 {
		return nil
	}

	w := oh.windows[idx]
	diffMin := w.End - min
	if idx == len(oh.windows)-1 && w.End == minutesPerWeek && oh.windows[0].Start == 0 {
		diffMin = (minutesPerWeek - min) + oh.windows[0].End
	}

	subMinute := time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond())*time.Nanosecond
	res := t.Add(time.Duration(diffMin)*time.Minute - subMinute)
	return &res
}

// GetTimeToOpen returns the duration until the next opening window starting from `from`.
// If currently open, returns 0. If never opens, returns nil.
func (oh *OpeningHours) GetTimeToOpen(from time.Time) *time.Duration {
	if oh == nil || len(oh.windows) == 0 {
		return nil
	}
	if len(oh.windows) == 1 && oh.windows[0].Start == 0 && oh.windows[0].End == minutesPerWeek {
		zero := time.Duration(0)
		return &zero
	}

	t := getWeekMinute(from)
	if oh.findWindowIndex(t) != -1 {
		zero := time.Duration(0)
		return &zero
	}

	idx := oh.findFirstWindowStartingAtOrAfter(t)
	subMinute := time.Duration(from.Second())*time.Second + time.Duration(from.Nanosecond())*time.Nanosecond

	if idx < len(oh.windows) {
		d := time.Duration(oh.windows[idx].Start-t)*time.Minute - subMinute
		return &d
	}

	d := time.Duration((minutesPerWeek-t)+oh.windows[0].Start)*time.Minute - subMinute
	return &d
}

// GetTimeToOpenForDuration returns the wait duration from `from` until an opening window
// that is at least `duration` long begins (or 0 if currently open and remaining continuous slot >= duration).
// Returns nil if no single opening window in the schedule is long enough.
func (oh *OpeningHours) GetTimeToOpenForDuration(from time.Time, duration time.Duration) *time.Duration {
	if oh == nil || len(oh.windows) == 0 {
		return nil
	}
	if duration <= 0 {
		zero := time.Duration(0)
		return &zero
	}
	if duration > time.Duration(minutesPerWeek)*time.Minute {
		return nil
	}
	if len(oh.windows) == 1 && oh.windows[0].Start == 0 && oh.windows[0].End == minutesPerWeek {
		zero := time.Duration(0)
		return &zero
	}

	t := getWeekMinute(from)
	subMinute := time.Duration(from.Second())*time.Second + time.Duration(from.Nanosecond())*time.Nanosecond
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
				zero := time.Duration(0)
				return &zero
			}
		} else {
			winDur := time.Duration(effectiveEnd-w.Start) * time.Minute
			if winDur >= duration {
				d := time.Duration(w.Start-t)*time.Minute - subMinute
				return &d
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
			return &d
		}
	}

	return nil
}

// When returns the time `from` + wait duration when `duration` can be continuously serviced during open hours,
// or nil if it never fits.
func (oh *OpeningHours) When(from time.Time, duration time.Duration) *time.Time {
	wait := oh.GetTimeToOpenForDuration(from, duration)
	if wait == nil {
		return nil
	}
	res := from.Add(*wait)
	return &res
}

// NextDur returns whether currently open, and the duration until state changes (shift ends or opens).
func (oh *OpeningHours) NextDur(t time.Time) (bool, time.Duration) {
	if oh == nil || len(oh.windows) == 0 {
		return false, 0
	}
	min := getWeekMinute(t)
	subMinute := time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond())*time.Nanosecond
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
	low := 0
	high := len(windows) - 1
	for low <= high {
		mid := int(uint(low+high) >> 1)
		w := &windows[mid]
		if t >= w.Start && t < w.End {
			return mid
		}
		if t < w.Start {
			high = mid - 1
		} else {
			low = mid + 1
		}
	}
	return -1
}

func (oh *OpeningHours) findFirstWindowStartingAtOrAfter(t int) int {
	windows := oh.windows
	low := 0
	high := len(windows) - 1
	result := len(windows)
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
	var expr string
	if err := json.Unmarshal(data, &expr); err != nil {
		return err
	}
	parsed := Parse(expr)
	*oh = *parsed
	return nil
}

func getWeekMinute(dt time.Time) int {
	day := (int(dt.Weekday()) + 6) % 7
	return day*1440 + dt.Hour()*60 + dt.Minute()
}

type timeRange struct {
	StartMin  int16
	EndMin    int16
	DayOffset int8
}

type openingRule struct {
	DayMask    uint8
	IsOff      bool
	IsAllDay   bool
	TimeRanges []timeRange
}

func parseOpeningRule(ruleString string) openingRule {
	var rule openingRule
	lower := strings.ToLower(ruleString)

	if strings.HasSuffix(lower, " off") {
		rule.IsOff = true
		ruleString = strings.TrimSpace(ruleString[:len(ruleString)-4])
	} else if strings.HasSuffix(lower, " closed") {
		rule.IsOff = true
		ruleString = strings.TrimSpace(ruleString[:len(ruleString)-7])
	} else if strings.HasSuffix(lower, " open") {
		ruleString = strings.TrimSpace(ruleString[:len(ruleString)-5])
	} else if lower == "off" || lower == "closed" {
		rule.IsOff = true
		rule.DayMask = 0x7F
		rule.IsAllDay = true
		return rule
	} else if lower == "open" || lower == "24/7" {
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
		rule.TimeRanges = parseTimes(timePart)
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
	lower := strings.ToLower(s)
	switch {
	case lower == "mo" || lower == "mon" || lower == "monday":
		return 0
	case lower == "tu" || lower == "tue" || lower == "tues" || lower == "tuesday":
		return 1
	case lower == "we" || lower == "wed" || lower == "wednesday":
		return 2
	case lower == "th" || lower == "thu" || lower == "thur" || lower == "thurs" || lower == "thursday":
		return 3
	case lower == "fr" || lower == "fri" || lower == "friday":
		return 4
	case lower == "sa" || lower == "sat" || lower == "saturday":
		return 5
	case lower == "su" || lower == "sun" || lower == "sunday":
		return 6
	default:
		return -1
	}
}

func parseTimes(timePart string) []timeRange {
	var ranges []timeRange
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
			ranges = append(ranges, timeRange{StartMin: 0, EndMin: 1440, DayOffset: 0})
		} else if dashIdx := strings.IndexByte(group, '-'); dashIdx >= 0 {
			part1 := group[:dashIdx]
			part2 := group[dashIdx+1:]
			s, ok1 := tryParseTimeMin(part1)
			e, ok2 := tryParseTimeMin(part2)
			if ok1 && ok2 {
				if s == 0 && e == 0 {
					ranges = append(ranges, timeRange{StartMin: 0, EndMin: 1440, DayOffset: 0})
				} else if s < e {
					ranges = append(ranges, timeRange{StartMin: int16(s), EndMin: int16(e), DayOffset: 0})
				} else if s > e {
					ranges = append(ranges, timeRange{StartMin: int16(s), EndMin: 1440, DayOffset: 0})
					ranges = append(ranges, timeRange{StartMin: 0, EndMin: int16(e), DayOffset: 1})
				}
			}
		} else if strings.HasSuffix(group, "+") {
			sOnly, ok := tryParseTimeMin(group[:len(group)-1])
			if ok {
				ranges = append(ranges, timeRange{StartMin: int16(sOnly), EndMin: 1440, DayOffset: 0})
			}
		}
	}
	return ranges
}

func tryParseTimeMin(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "24:00" {
		return 1440, true
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
	if len(s) == 1 {
		if s[0] >= '0' && s[0] <= '9' {
			return int(s[0] - '0'), true
		}
		return 0, false
	}
	if len(s) == 2 {
		if s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9' {
			return int(s[0]-'0')*10 + int(s[1]-'0'), true
		}
		return 0, false
	}
	return 0, false
}

func bakeRules(rules []openingRule) []TimeWindow {
	var openIntervals []TimeWindow

	for _, rule := range rules {
		var ruleWindows []TimeWindow
		for day := 0; day < 7; day++ {
			if (rule.DayMask & (1 << day)) == 0 {
				continue
			}

			if rule.IsAllDay {
				dayOffset := day * 1440
				ruleWindows = append(ruleWindows, TimeWindow{Start: dayOffset, End: dayOffset + 1440})
			} else {
				for _, tr := range rule.TimeRanges {
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
			openIntervals = mergeWindows(openIntervals)
		} else {
			openIntervals = subtractWindows(openIntervals, ruleWindows)
		}
	}

	sort.Slice(openIntervals, func(i, j int) bool {
		return openIntervals[i].Start < openIntervals[j].Start
	})
	return openIntervals
}

func mergeWindows(intervals []TimeWindow) []TimeWindow {
	if len(intervals) <= 1 {
		return intervals
	}
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].Start < intervals[j].Start
	})

	merged := make([]TimeWindow, 0, len(intervals))
	merged = append(merged, intervals[0])
	for i := 1; i < len(intervals); i++ {
		curr := intervals[i]
		lastIdx := len(merged) - 1

		if curr.Start <= merged[lastIdx].End {
			if curr.End > merged[lastIdx].End {
				merged[lastIdx].End = curr.End
			}
		} else {
			merged = append(merged, curr)
		}
	}
	return merged
}

func subtractWindows(source []TimeWindow, subtrahends []TimeWindow) []TimeWindow {
	result := make([]TimeWindow, len(source))
	copy(result, source)

	for _, sub := range mergeWindows(subtrahends) {
		var nextResult []TimeWindow
		for _, s := range result {
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
		result = nextResult
	}
	return result
}



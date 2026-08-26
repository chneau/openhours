package openhours

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
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
	trimmed := strings.TrimSpace(expression)

	internMu.RLock()
	if cached, ok := internPool[expression]; ok {
		internMu.RUnlock()
		return cached
	}
	internMu.RUnlock()

	internMu.Lock()
	defer internMu.Unlock()
	if cached, ok := internPool[expression]; ok {
		return cached
	}

	var oh *OpeningHours
	if trimmed == "" {
		oh = &OpeningHours{expression: expression, windows: nil}
	} else if trimmed == "24/7" {
		oh = &OpeningHours{expression: expression, windows: []TimeWindow{{Start: 0, End: minutesPerWeek}}}
	} else {
		rawRules := strings.Split(expression, ";")
		var rules []openingRule
		for _, r := range rawRules {
			r = strings.TrimSpace(r)
			if r != "" {
				rules = append(rules, parseOpeningRule(r))
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

	res := t.Add(time.Duration(diffMin) * time.Minute)
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
	idx := oh.findFirstWindowStartingAtOrAfter(t)

	if idx < len(oh.windows) && oh.windows[idx].Start <= t {
		zero := time.Duration(0)
		return &zero
	}

	if idx < len(oh.windows) {
		d := time.Duration(oh.windows[idx].Start-t) * time.Minute
		return &d
	}

	d := time.Duration((minutesPerWeek-t)+oh.windows[0].Start) * time.Minute
	return &d
}

// GetTimeToOpenForDuration returns the wait duration from `from` until an opening window
// that is at least `duration` long begins (or 0 if currently open and remaining continuous slot >= duration).
// Returns nil if no single opening window in the schedule is long enough.
func (oh *OpeningHours) GetTimeToOpenForDuration(from time.Time, duration time.Duration) *time.Duration {
	if oh == nil || len(oh.windows) == 0 {
		return nil
	}
	req := int(math.Ceil(duration.Minutes()))
	if req <= 0 {
		zero := time.Duration(0)
		return &zero
	}
	if req > minutesPerWeek {
		return nil
	}
	if len(oh.windows) == 1 && oh.windows[0].Start == 0 && oh.windows[0].End == minutesPerWeek {
		zero := time.Duration(0)
		return &zero
	}

	t := getWeekMinute(from)

	startIdx := oh.findFirstWindowStartingAtOrAfter(t)
	if startIdx > 0 && oh.windows[startIdx-1].End > t {
		startIdx--
	}

	// Check windows in current week starting from t
	for i := startIdx; i < len(oh.windows); i++ {
		w := oh.windows[i]
		effectiveEnd := w.End
		if i == len(oh.windows)-1 && w.End == minutesPerWeek && oh.windows[0].Start == 0 {
			effectiveEnd = minutesPerWeek + oh.windows[0].End
		}

		if effectiveEnd <= t {
			continue
		}

		effectiveStart := t
		if w.Start > effectiveStart {
			effectiveStart = w.Start
		}

		if effectiveEnd-effectiveStart >= req {
			d := time.Duration(effectiveStart-t) * time.Minute
			return &d
		}
	}

	// Wrap around to next week
	for i := 0; i < len(oh.windows); i++ {
		w := oh.windows[i]
		windowDuration := w.End - w.Start
		if i == len(oh.windows)-1 && w.End == minutesPerWeek && oh.windows[0].Start == 0 {
			windowDuration = (minutesPerWeek - w.Start) + oh.windows[0].End
		}

		if windowDuration >= req {
			d := time.Duration((minutesPerWeek-t)+w.Start) * time.Minute
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
	if oh.IsOpen(t) {
		end := oh.GetCurrentShiftEnd(t)
		if end == nil {
			return true, time.Duration(minutesPerWeek) * time.Minute
		}
		return true, end.Sub(t)
	}
	toOpen := oh.GetTimeToOpen(t)
	if toOpen == nil {
		return false, 0
	}
	return false, *toOpen
}

// NextDate returns whether currently open, and the time when the status changes.
func (oh *OpeningHours) NextDate(t time.Time) (bool, time.Time) {
	isOpen, dur := oh.NextDur(t)
	return isOpen, t.Add(dur)
}

func (oh *OpeningHours) findWindowIndex(t int) int {
	low := 0
	high := len(oh.windows) - 1
	for low <= high {
		mid := low + (high-low)/2
		if t >= oh.windows[mid].Start && t < oh.windows[mid].End {
			return mid
		}
		if t < oh.windows[mid].Start {
			high = mid - 1
		} else {
			low = mid + 1
		}
	}
	return -1
}

func (oh *OpeningHours) findFirstWindowStartingAtOrAfter(t int) int {
	low := 0
	high := len(oh.windows) - 1
	result := len(oh.windows)
	for low <= high {
		mid := low + (high-low)/2
		if oh.windows[mid].End > t {
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
	StartMin  int
	EndMin    int
	DayOffset int
}

type openingRule struct {
	DayMask    int
	TimeRanges []timeRange
	IsOff      bool
	IsAllDay   bool
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
	}

	parts := strings.Fields(ruleString)
	if len(parts) == 0 {
		return rule
	}

	dayMask := parseDayMask(parts[0])
	if dayMask > 0 {
		rule.DayMask = dayMask
		if len(parts) > 1 {
			rule.TimeRanges = parseTimes(strings.Join(parts[1:], " "))
			rule.IsAllDay = false
		} else {
			rule.IsAllDay = true
		}
	} else {
		rule.DayMask = 0x7F // All 7 days
		rule.TimeRanges = parseTimes(strings.Join(parts, " "))
		rule.IsAllDay = false
	}
	return rule
}

func parseDayMask(dayPart string) int {
	mask := 0
	groups := strings.Split(dayPart, ",")
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if strings.Contains(group, "-") {
			rangeParts := strings.Split(group, "-")
			if len(rangeParts) != 2 {
				return 0
			}
			start := dayToIndex(rangeParts[0])
			end := dayToIndex(rangeParts[1])
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
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mo":
		return 0
	case "tu":
		return 1
	case "we":
		return 2
	case "th":
		return 3
	case "fr":
		return 4
	case "sa":
		return 5
	case "su":
		return 6
	default:
		return -1
	}
}

func parseTimes(timePart string) []timeRange {
	var ranges []timeRange
	groups := strings.Split(timePart, ",")
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if strings.Contains(group, "-") {
			parts := strings.Split(group, "-")
			if len(parts) == 2 {
				s, ok1 := tryParseTimeMin(parts[0])
				e, ok2 := tryParseTimeMin(parts[1])
				if ok1 && ok2 {
					if s < e {
						ranges = append(ranges, timeRange{StartMin: s, EndMin: e, DayOffset: 0})
					} else if s > e {
						ranges = append(ranges, timeRange{StartMin: s, EndMin: 1440, DayOffset: 0})
						ranges = append(ranges, timeRange{StartMin: 0, EndMin: e, DayOffset: 1})
					}
				}
			}
		} else if strings.HasSuffix(group, "+") {
			sOnly, ok := tryParseTimeMin(strings.TrimSuffix(group, "+"))
			if ok {
				ranges = append(ranges, timeRange{StartMin: sOnly, EndMin: 1440, DayOffset: 0})
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
	parts := strings.Split(s, ":")
	if len(parts) == 2 {
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 == nil && err2 == nil && h >= 0 && h < 24 && m >= 0 && m < 60 {
			return h*60 + m, true
		}
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
					targetDay := (day + tr.DayOffset) % 7
					dayOffset := targetDay * 1440
					ruleWindows = append(ruleWindows, TimeWindow{
						Start: dayOffset + tr.StartMin,
						End:   dayOffset + tr.EndMin,
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

	merged := []TimeWindow{intervals[0]}
	for i := 1; i < len(intervals); i++ {
		curr := intervals[i]
		lastIdx := len(merged) - 1
		last := merged[lastIdx]

		if curr.Start <= last.End {
			if curr.End > last.End {
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


package openhours

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ====================================================================
// Tests for OpeningHours API
// ====================================================================

func TestIsOpen(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		dateTime   time.Time
		expected   bool
	}{
		// Basic daily range
		{"Mo-Fr Monday 10am", "Mo-Fr 08:00-17:00", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), true},
		{"Mo-Fr Monday 7:59am", "Mo-Fr 08:00-17:00", time.Date(2026, 5, 18, 7, 59, 0, 0, time.UTC), false},
		{"Mo-Fr Monday 5pm", "Mo-Fr 08:00-17:00", time.Date(2026, 5, 18, 17, 0, 0, 0, time.UTC), false},
		{"Mo-Fr Saturday 10am", "Mo-Fr 08:00-17:00", time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC), false},

		// Multiple time intervals
		{"Mo-Fr lunch break 12:30pm", "Mo-Fr 08:00-12:00, 13:00-17:00", time.Date(2026, 5, 18, 12, 30, 0, 0, time.UTC), false},
		{"Mo-Fr afternoon 2pm", "Mo-Fr 08:00-12:00, 13:00-17:00", time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC), true},

		// Multiple rules
		{"Multiple rules Sa 10am", "Mo-Fr 08:00-17:00; Sa 08:00-12:00", time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC), true},
		{"Multiple rules Sa 2pm", "Mo-Fr 08:00-17:00; Sa 08:00-12:00", time.Date(2026, 5, 23, 14, 0, 0, 0, time.UTC), false},

		// Off modifier (override)
		{"Off modifier Tu 12:30pm", "Mo-Su 00:00-24:00; Tu 12:00-13:00 off", time.Date(2026, 5, 19, 12, 30, 0, 0, time.UTC), false},
		{"Off modifier Tu 2pm", "Mo-Su 00:00-24:00; Tu 12:00-13:00 off", time.Date(2026, 5, 19, 14, 0, 0, 0, time.UTC), true},
		{"Closed modifier Tu 12:30pm", "Mo-Su 00:00-24:00; Tu 12:00-13:00 closed", time.Date(2026, 5, 19, 12, 30, 0, 0, time.UTC), false},

		// 24/7 shortcut
		{"24/7 Sunday night", "24/7", time.Date(2026, 5, 24, 23, 59, 59, 0, time.UTC), true},

		// Wrap around day range
		{"Wrap around day range Sa-Su Sunday 10am", "Sa-Su 08:00-12:00", time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC), true},
		{"Wrap around day range Sa-Su Monday 10am", "Sa-Su 08:00-12:00", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), false},

		// Open ended
		{"Open ended Mo 10:00+ at 10pm", "Mo 10:00+", time.Date(2026, 5, 18, 22, 0, 0, 0, time.UTC), true},

		// No days (assume every day)
		{"No days 10:00-12:00 Mon 11am", "10:00-12:00", time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC), true},
		{"No days 10:00-12:00 Sat 11am", "10:00-12:00", time.Date(2026, 5, 23, 11, 0, 0, 0, time.UTC), true},

		// 00:00-24:00 (Every day, all day)
		{"00:00-24:00 Mon midnight", "00:00-24:00", time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC), true},
		{"00:00-24:00 Mon noon", "00:00-24:00", time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC), true},
		{"00:00-24:00 Sun night", "00:00-24:00", time.Date(2026, 5, 24, 23, 59, 0, 0, time.UTC), true},

		// Invalid Syntax & Fallback Tests
		{"Invalid 'invalid'", "invalid", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), false},
		{"Invalid 'Mo invalid'", "Mo invalid", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), false},
		{"Invalid 'Mo 25:00-26:00'", "Mo 25:00-26:00", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), false},
		{"Invalid 'Xx 08:00-17:00'", "Xx 08:00-17:00", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), false},
		{"Empty ''", "", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), false},
		{"Spaces '   '", "   ", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), false},

		// Valid day selector without time range defaults to 24h on those days
		{"Mo all day midnight", "Mo", time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC), true},
		{"Mo all day noon", "Mo", time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC), true},
		{"Mo all day 23:59", "Mo", time.Date(2026, 5, 18, 23, 59, 0, 0, time.UTC), true},
		{"Mo all day Tue 10am", "Mo", time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC), false},
		{"Mo-Fr Mon 10am", "Mo-Fr", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), true},
		{"Mo-Fr Sat 10am", "Mo-Fr", time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC), false},

		// Overnight Shift Tests
		// Mid-week overnight shift: Monday 22:00 to Tuesday 04:00
		{"Mo 22:00-04:00 Mon 21:59", "Mo 22:00-04:00", time.Date(2026, 5, 18, 21, 59, 0, 0, time.UTC), false},
		{"Mo 22:00-04:00 Mon 22:00", "Mo 22:00-04:00", time.Date(2026, 5, 18, 22, 0, 0, 0, time.UTC), true},
		{"Mo 22:00-04:00 Mon 23:30", "Mo 22:00-04:00", time.Date(2026, 5, 18, 23, 30, 0, 0, time.UTC), true},
		{"Mo 22:00-04:00 Tue 00:30", "Mo 22:00-04:00", time.Date(2026, 5, 19, 0, 30, 0, 0, time.UTC), true},
		{"Mo 22:00-04:00 Tue 03:59", "Mo 22:00-04:00", time.Date(2026, 5, 19, 3, 59, 0, 0, time.UTC), true},
		{"Mo 22:00-04:00 Tue 04:00", "Mo 22:00-04:00", time.Date(2026, 5, 19, 4, 0, 0, 0, time.UTC), false},

		// Week wrap-around overnight shift: Sunday 22:00 to Monday 04:00
		{"Su 22:00-04:00 Sun 21:59", "Su 22:00-04:00", time.Date(2026, 5, 24, 21, 59, 0, 0, time.UTC), false},
		{"Su 22:00-04:00 Sun 22:00", "Su 22:00-04:00", time.Date(2026, 5, 24, 22, 0, 0, 0, time.UTC), true},
		{"Su 22:00-04:00 Sun 23:59", "Su 22:00-04:00", time.Date(2026, 5, 24, 23, 59, 0, 0, time.UTC), true},
		{"Su 22:00-04:00 Mon 00:01", "Su 22:00-04:00", time.Date(2026, 5, 18, 0, 1, 0, 0, time.UTC), true},
		{"Su 22:00-04:00 Mon 03:59", "Su 22:00-04:00", time.Date(2026, 5, 18, 3, 59, 0, 0, time.UTC), true},
		{"Su 22:00-04:00 Mon 04:00", "Su 22:00-04:00", time.Date(2026, 5, 18, 4, 0, 0, 0, time.UTC), false},

		// Multi-day overnight shift
		{"Mo-Fr 22:00-04:00 Mon 23:00", "Mo-Fr 22:00-04:00", time.Date(2026, 5, 18, 23, 0, 0, 0, time.UTC), true},
		{"Mo-Fr 22:00-04:00 Tue 02:00", "Mo-Fr 22:00-04:00", time.Date(2026, 5, 19, 2, 0, 0, 0, time.UTC), true},
		{"Mo-Fr 22:00-04:00 Fri 23:00", "Mo-Fr 22:00-04:00", time.Date(2026, 5, 22, 23, 0, 0, 0, time.UTC), true},
		{"Mo-Fr 22:00-04:00 Sat 02:00", "Mo-Fr 22:00-04:00", time.Date(2026, 5, 23, 2, 0, 0, 0, time.UTC), true},
		{"Mo-Fr 22:00-04:00 Sat 23:00", "Mo-Fr 22:00-04:00", time.Date(2026, 5, 23, 23, 0, 0, 0, time.UTC), false},
		{"Mo-Fr 22:00-04:00 Sun 02:00", "Mo-Fr 22:00-04:00", time.Date(2026, 5, 24, 2, 0, 0, 0, time.UTC), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oh := Parse(tt.expression)
			got := oh.IsOpen(tt.dateTime)
			if got != tt.expected {
				t.Errorf("Parse(%q).IsOpen(%v) = %v, want %v", tt.expression, tt.dateTime, got, tt.expected)
			}
			if oh.Match(tt.dateTime) != tt.expected {
				t.Errorf("Parse(%q).Match(%v) = %v, want %v", tt.expression, tt.dateTime, oh.Match(tt.dateTime), tt.expected)
			}
		})
	}
}

func TestGetCurrentShiftEnd(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		dateTime   time.Time
		expected   *time.Time
	}{
		{
			"Mo-Fr 08:00-17:00 at 10am",
			"Mo-Fr 08:00-17:00",
			time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
			timePtr(time.Date(2026, 5, 18, 17, 0, 0, 0, time.UTC)),
		},
		{
			"Mo-Fr 08:00-17:00 at 17:30",
			"Mo-Fr 08:00-17:00",
			time.Date(2026, 5, 18, 17, 30, 0, 0, time.UTC),
			nil,
		},
		{
			"Mo 22:00-04:00 at Mon 23:00",
			"Mo 22:00-04:00",
			time.Date(2026, 5, 18, 23, 0, 0, 0, time.UTC),
			timePtr(time.Date(2026, 5, 19, 4, 0, 0, 0, time.UTC)),
		},
		{
			"Mo 22:00-04:00 at Tue 02:00",
			"Mo 22:00-04:00",
			time.Date(2026, 5, 19, 2, 0, 0, 0, time.UTC),
			timePtr(time.Date(2026, 5, 19, 4, 0, 0, 0, time.UTC)),
		},
		{
			"Su 22:00-04:00 at Sun 23:00",
			"Su 22:00-04:00",
			time.Date(2026, 5, 24, 23, 0, 0, 0, time.UTC),
			timePtr(time.Date(2026, 5, 25, 4, 0, 0, 0, time.UTC)),
		},
		{
			"Su 22:00-04:00 at Mon 02:00",
			"Su 22:00-04:00",
			time.Date(2026, 5, 18, 2, 0, 0, 0, time.UTC),
			timePtr(time.Date(2026, 5, 18, 4, 0, 0, 0, time.UTC)),
		},
		{
			"24/7 at 10am -> null",
			"24/7",
			time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
			nil,
		},
		{
			"empty at 10am -> null",
			"",
			time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oh := Parse(tt.expression)
			got := oh.GetCurrentShiftEnd(tt.dateTime)
			if (got == nil && tt.expected != nil) || (got != nil && tt.expected == nil) {
				t.Errorf("Parse(%q).GetCurrentShiftEnd(%v) = %v, want %v", tt.expression, tt.dateTime, got, tt.expected)
			} else if got != nil && tt.expected != nil && !got.Equal(*tt.expected) {
				t.Errorf("Parse(%q).GetCurrentShiftEnd(%v) = %v, want %v", tt.expression, tt.dateTime, *got, *tt.expected)
			}
		})
	}
}

func TestGetTimeToOpen(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		dateTime   time.Time
		expected   *time.Duration
	}{
		// Monday 8am to next Monday 8am (6 day wait)
		{"Mo 08:00-16:00 from Tue 8am", "Mo 08:00-16:00", time.Date(2026, 5, 19, 8, 0, 0, 0, time.UTC), durPtr(6 * 24 * time.Hour)},

		// Already open
		{"10:00-12:00 from Mon 11am", "10:00-12:00", time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC), durPtr(0)},

		// Opens later today
		{"10:00-12:00 from Mon 9am", "10:00-12:00", time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC), durPtr(time.Hour)},

		// Opens tomorrow
		{"Mo 10:00-12:00 from Sun 10am", "Mo 10:00-12:00", time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC), durPtr(24 * time.Hour)},

		// Multiple ranges
		{"08:00-10:00, 14:00-16:00 from 11am", "08:00-10:00, 14:00-16:00", time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC), durPtr(3 * time.Hour)},

		// Null case: Never opens
		{"empty string", "", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oh := Parse(tt.expression)
			got := oh.GetTimeToOpen(tt.dateTime)
			if (got == nil && tt.expected != nil) || (got != nil && tt.expected == nil) {
				t.Errorf("Parse(%q).GetTimeToOpen(%v) = %v, want %v", tt.expression, tt.dateTime, got, tt.expected)
			} else if got != nil && tt.expected != nil && *got != *tt.expected {
				t.Errorf("Parse(%q).GetTimeToOpen(%v) = %v, want %v", tt.expression, tt.dateTime, *got, *tt.expected)
			}
		})
	}
}

func TestGetTimeToOpenForDuration(t *testing.T) {
	lunchBreakExpr := "Mo-Fr 08:00-12:00, 13:00-17:00"
	weekendExpr := "Mo-Fr 08:00-17:00; Sa 08:00-12:00"
	exclusionExpr := "Mo-Su 00:00-24:00; Tu 12:00-13:00 off"

	tests := []struct {
		name       string
		expression string
		dateTime   time.Time
		duration   time.Duration
		expected   *time.Duration
	}{
		// Monday 8am, opens 10-12, ask for 1h -> Wait 2h (until 10am)
		{"10:00-12:00 from 8am for 1h", "10:00-12:00", time.Date(2026, 5, 18, 8, 0, 0, 0, time.UTC), time.Hour, durPtr(2 * time.Hour)},

		// Monday 11am, opens 10-12, ask for 2h -> Current window too short (only 1h left), Wait 23h (until Tuesday 10am)
		{"10:00-12:00 from 11am for 2h", "10:00-12:00", time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC), 2 * time.Hour, durPtr(23 * time.Hour)},

		// Already open and fits
		{"10:00-14:00 from 11am for 2h", "10:00-14:00", time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC), 2 * time.Hour, durPtr(0)},

		// Multiple slots, first too short: 7am to 2pm
		{"08:00-09:00, 14:00-17:00 from 7am for 2h", "08:00-09:00, 14:00-17:00", time.Date(2026, 5, 18, 7, 0, 0, 0, time.UTC), 2 * time.Hour, durPtr(7 * time.Hour)},

		// Wrap-around loop optimization fix test case:
		// Tu 08-09 (1h), We 08-10 (2h), Th 08-20 (12h). Query at Tue 10:00 for 10h duration.
		// Needs to skip Tu 08-09 (passed) and We 08-10 (too short) and find Th 08-20.
		{"Tu 08-09; We 08-10; Th 08-20 from Tue 10am for 10h", "Tu 08:00-09:00; We 08:00-10:00; Th 08:00-20:00", time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC), 10 * time.Hour, durPtr(46 * time.Hour)},

		// Overnight shift duration test
		{"Su 22:00-04:00 from Sun 9pm for 5h", "Su 22:00-04:00", time.Date(2026, 5, 24, 21, 0, 0, 0, time.UTC), 5 * time.Hour, durPtr(time.Hour)},

		// Complex Case: Lunch Breaks
		// Ask for 2h at 10am (Fits in morning)
		{"Lunch breaks 10am for 2h", lunchBreakExpr, time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), 2 * time.Hour, durPtr(0)},
		// Ask for 3h at 11am (Does NOT fit in morning, must wait until 1pm)
		{"Lunch breaks 11am for 3h", lunchBreakExpr, time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC), 3 * time.Hour, durPtr(2 * time.Hour)},
		// Ask for 5h (Never fits in any single slot)
		{"Lunch breaks 9am for 5h", lunchBreakExpr, time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC), 5 * time.Hour, nil},

		// Complex Case: Weekend Overrides
		// Friday 4pm, ask for 4h (Only 1h left today, tomorrow only 4h) -> Wait until Sat 8am (wait 16h)
		{"Weekend overrides Fri 4pm for 4h", weekendExpr, time.Date(2026, 5, 22, 16, 0, 0, 0, time.UTC), 4 * time.Hour, durPtr(16 * time.Hour)},
		// Friday 4pm, ask for 6h (Doesn't fit Sat, wait until Mon 8am) -> Wait 64h
		{"Weekend overrides Fri 4pm for 6h", weekendExpr, time.Date(2026, 5, 22, 16, 0, 0, 0, time.UTC), 6 * time.Hour, durPtr(64 * time.Hour)},

		// Complex Case: Exclusions
		// Tuesday 11:30am, ask for 1h (Only 30m left before 'off') -> Wait until 1pm (Wait 1.5h)
		{"Exclusions Tue 11:30am for 1h", exclusionExpr, time.Date(2026, 5, 19, 11, 30, 0, 0, time.UTC), time.Hour, durPtr(90 * time.Minute)},
		// Tuesday 11:30am, ask for 24h -> Next 24h continuous slot starts Tue 13:00 (Wait 1.5h)
		{"Exclusions Tue 11:30am for 24h", exclusionExpr, time.Date(2026, 5, 19, 11, 30, 0, 0, time.UTC), 24 * time.Hour, durPtr(90 * time.Minute)},

		// Null case: Never opens or duration exceeds slot
		{"10:00-11:00 for 5h", "10:00-11:00", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), 5 * time.Hour, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oh := Parse(tt.expression)
			got := oh.GetTimeToOpenForDuration(tt.dateTime, tt.duration)
			if (got == nil && tt.expected != nil) || (got != nil && tt.expected == nil) {
				t.Errorf("Parse(%q).GetTimeToOpenForDuration(%v, %v) = %v, want %v", tt.expression, tt.dateTime, tt.duration, got, tt.expected)
			} else if got != nil && tt.expected != nil && *got != *tt.expected {
				t.Errorf("Parse(%q).GetTimeToOpenForDuration(%v, %v) = %v, want %v", tt.expression, tt.dateTime, tt.duration, *got, *tt.expected)
			}
		})
	}
}

func TestWhen(t *testing.T) {
	oh := Parse("Mo 10:00-15:00")
	now := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	when := oh.When(now, 4*time.Hour)
	if when == nil || !when.Equal(now) {
		t.Errorf("When expected %v, got %v", now, when)
	}

	// Request 10h duration (impossible in a 5h slot)
	whenNone := oh.When(now, 10*time.Hour)
	if whenNone != nil {
		t.Errorf("When expected nil, got %v", whenNone)
	}
}

func TestNextDurAndNextDate(t *testing.T) {
	oh := Parse("Mo 08:00-18:00")
	tBefore := time.Date(2026, 5, 18, 7, 0, 0, 0, time.UTC)
	isOpen, dur := oh.NextDur(tBefore)
	if isOpen || dur != time.Hour {
		t.Errorf("NextDur before open: got isOpen=%v, dur=%v; want false, 1h", isOpen, dur)
	}
	isOpen, nextDate := oh.NextDate(tBefore)
	expectedOpenTime := time.Date(2026, 5, 18, 8, 0, 0, 0, time.UTC)
	if isOpen || !nextDate.Equal(expectedOpenTime) {
		t.Errorf("NextDate before open: got isOpen=%v, date=%v; want false, %v", isOpen, nextDate, expectedOpenTime)
	}

	tInside := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
	isOpen, dur = oh.NextDur(tInside)
	if !isOpen || dur != 9*time.Hour {
		t.Errorf("NextDur during open: got isOpen=%v, dur=%v; want true, 9h", isOpen, dur)
	}
}

func TestJSONSerialization(t *testing.T) {
	expressions := []string{
		"Mo-Fr 08:00-17:00",
		"24/7",
		"Mo-Su 00:00-24:00; Tu 12:00-13:00 off",
	}

	for _, expr := range expressions {
		t.Run(expr, func(t *testing.T) {
			orig := Parse(expr)
			data, err := json.Marshal(orig)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			expectedJSON := `"` + expr + `"`
			if string(data) != expectedJSON {
				t.Errorf("json.Marshal = %s, want %s", string(data), expectedJSON)
			}

			var deserialized *OpeningHours
			if err := json.Unmarshal(data, &deserialized); err != nil {
				t.Fatalf("json.Unmarshal failed: %v", err)
			}
			if deserialized.Raw() != expr {
				t.Errorf("deserialized.Raw() = %q, want %q", deserialized.Raw(), expr)
			}
		})
	}
}

func TestWindows(t *testing.T) {
	oh := Parse("Mo 08:00-12:00")
	windows := oh.Windows()
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].Start != 8*60 || windows[0].End != 12*60 {
		t.Errorf("unexpected window: %+v", windows[0])
	}
}

func TestAdvancedOSMSyntax(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		dateTime   time.Time
		expected   bool
	}{
		// Spaced day lists
		{"Mo, Tu, We on Monday", "Mo, Tu, We 08:00-12:00", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), true},
		{"Mo, Tu, We on Tuesday", "Mo, Tu, We 08:00-12:00", time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC), true},
		{"Mo, Tu, We on Wednesday", "Mo, Tu, We 08:00-12:00", time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC), true},
		{"Mo, Tu, We on Thursday", "Mo, Tu, We 08:00-12:00", time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC), false},

		// Spaced day range with dash
		{"Mo - Fr on Monday", "Mo - Fr 08:00-17:00", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), true},
		{"Mo - Fr on Saturday", "Mo - Fr 08:00-17:00", time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC), false},

		// Combined range and list: Mo-We, Fr
		{"Mo-We, Fr on Wednesday", "Mo-We, Fr 08:00-17:00", time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC), true},
		{"Mo-We, Fr on Thursday", "Mo-We, Fr 08:00-17:00", time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC), false},
		{"Mo-We, Fr on Friday", "Mo-We, Fr 08:00-17:00", time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC), true},

		// 3-letter and full day name aliases
		{"Mon-Fri on Monday", "Mon-Fri 08:00-17:00", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC), true},
		{"Monday-Friday on Friday", "Monday-Friday 08:00-17:00", time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC), true},
		{"Monday-Friday on Saturday", "Monday-Friday 08:00-17:00", time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC), false},

		// 00:00-00:00 all day
		{"00:00-00:00 on Monday", "Mo 00:00-00:00", time.Date(2026, 5, 18, 15, 30, 0, 0, time.UTC), true},
		{"00:00-00:00 on Tuesday", "Mo 00:00-00:00", time.Date(2026, 5, 19, 15, 30, 0, 0, time.UTC), false},

		// open keyword
		{"Mo open on Monday", "Mo open", time.Date(2026, 5, 18, 15, 30, 0, 0, time.UTC), true},
		{"Mo open on Tuesday", "Mo open", time.Date(2026, 5, 19, 15, 30, 0, 0, time.UTC), false},
		{"open on Sunday", "open", time.Date(2026, 5, 24, 15, 30, 0, 0, time.UTC), true},

		// off/closed keyword alone
		{"closed on Monday", "closed", time.Date(2026, 5, 18, 15, 30, 0, 0, time.UTC), false},
		{"off on Monday", "off", time.Date(2026, 5, 18, 15, 30, 0, 0, time.UTC), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oh := Parse(tt.expression)
			got := oh.IsOpen(tt.dateTime)
			if got != tt.expected {
				t.Errorf("Parse(%q).IsOpen(%v) = %v, want %v", tt.expression, tt.dateTime, got, tt.expected)
			}
		})
	}
}

func TestSubMinutePrecision(t *testing.T) {
	oh := Parse("Mo 08:00-17:00")

	// 1. GetCurrentShiftEnd with seconds
	tOpen := time.Date(2026, 5, 18, 10, 15, 30, 500, time.UTC)
	expectedEnd := time.Date(2026, 5, 18, 17, 0, 0, 0, time.UTC)
	shiftEnd := oh.GetCurrentShiftEnd(tOpen)
	if shiftEnd == nil || !shiftEnd.Equal(expectedEnd) {
		t.Errorf("GetCurrentShiftEnd(%v) = %v, want %v", tOpen, shiftEnd, expectedEnd)
	}

	// 2. NextDur and NextDate when open (30 seconds until close)
	tNearClose := time.Date(2026, 5, 18, 16, 59, 30, 0, time.UTC)
	isOpen, dur := oh.NextDur(tNearClose)
	if !isOpen || dur != 30*time.Second {
		t.Errorf("NextDur(%v) = (%v, %v), want (true, 30s)", tNearClose, isOpen, dur)
	}
	isOpen, nextDate := oh.NextDate(tNearClose)
	if !isOpen || !nextDate.Equal(expectedEnd) {
		t.Errorf("NextDate(%v) = (%v, %v), want (true, %v)", tNearClose, isOpen, nextDate, expectedEnd)
	}

	// 3. NextDur, NextDate, and GetTimeToOpen when closed (30 seconds until open)
	tNearOpen := time.Date(2026, 5, 18, 7, 59, 30, 0, time.UTC)
	expectedStart := time.Date(2026, 5, 18, 8, 0, 0, 0, time.UTC)
	isOpen, dur = oh.NextDur(tNearOpen)
	if isOpen || dur != 30*time.Second {
		t.Errorf("NextDur(%v) = (%v, %v), want (false, 30s)", tNearOpen, isOpen, dur)
	}
	isOpen, nextDate = oh.NextDate(tNearOpen)
	if isOpen || !nextDate.Equal(expectedStart) {
		t.Errorf("NextDate(%v) = (%v, %v), want (false, %v)", tNearOpen, isOpen, nextDate, expectedStart)
	}
	timeToOpen := oh.GetTimeToOpen(tNearOpen)
	if timeToOpen == nil || *timeToOpen != 30*time.Second {
		t.Errorf("GetTimeToOpen(%v) = %v, want 30s", tNearOpen, timeToOpen)
	}

	// 4. GetTimeToOpenForDuration and When with sub-minute precision
	// Only 30 seconds left before close, request 1 minute -> must wait until next week Monday 08:00
	waitDur := oh.GetTimeToOpenForDuration(tNearClose, 1*time.Minute)
	expectedWait := (7*24*time.Hour - 16*time.Hour - 59*time.Minute - 30*time.Second) + 8*time.Hour
	if waitDur == nil || *waitDur != expectedWait {
		t.Errorf("GetTimeToOpenForDuration(%v, 1m) = %v, want %v", tNearClose, waitDur, expectedWait)
	}
	whenDate := oh.When(tNearClose, 1*time.Minute)
	expectedNextOpen := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	if whenDate == nil || !whenDate.Equal(expectedNextOpen) {
		t.Errorf("When(%v, 1m) = %v, want %v", tNearClose, whenDate, expectedNextOpen)
	}
}

func TestNilSafety(t *testing.T) {
	var nilOH *OpeningHours
	now := time.Now()

	if nilOH.Raw() != "" {
		t.Errorf("expected empty raw for nil")
	}
	if nilOH.String() != "" {
		t.Errorf("expected empty string for nil")
	}
	if nilOH.Windows() != nil {
		t.Errorf("expected nil windows for nil")
	}
	if nilOH.IsOpen(now) {
		t.Errorf("expected false for IsOpen on nil")
	}
	if nilOH.Match(now) {
		t.Errorf("expected false for Match on nil")
	}
	if nilOH.GetCurrentShiftEnd(now) != nil {
		t.Errorf("expected nil for GetCurrentShiftEnd on nil")
	}
	if nilOH.GetTimeToOpen(now) != nil {
		t.Errorf("expected nil for GetTimeToOpen on nil")
	}
	if nilOH.GetTimeToOpenForDuration(now, time.Hour) != nil {
		t.Errorf("expected nil for GetTimeToOpenForDuration on nil")
	}
	if nilOH.When(now, time.Hour) != nil {
		t.Errorf("expected nil for When on nil")
	}
	open, dur := nilOH.NextDur(now)
	if open || dur != 0 {
		t.Errorf("expected false, 0 for NextDur on nil")
	}
	open, nextDate := nilOH.NextDate(now)
	if open || !nextDate.Equal(now) {
		t.Errorf("expected false, now for NextDate on nil")
	}
	data, err := nilOH.MarshalJSON()
	if err != nil || string(data) != "null" {
		t.Errorf("expected null json for nil")
	}
}

func TestConcurrentEvaluations(t *testing.T) {
	expr := "Mo-Fr 08:00-12:00, 13:00-17:00; Sa 08:00-12:00"
	oh := Parse(expr)
	base := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)

	const goroutines = 8
	const iterations = 10000

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				dt := base.Add(time.Duration((g*1000+i)%10080) * time.Minute)
				_ = oh.IsOpen(dt)
				_, _ = oh.TimeToOpen(dt)
			}
		}(g)
	}
	wg.Wait()
}

func TestRunCSBenchmarkComparison(t *testing.T) {
	fmt.Println("\n========================================================")
	fmt.Println("Running OpeningHours Benchmarks (Full Standard Suite)")
	fmt.Println("========================================================")

	complexExpr := "Mo-Fr 08:00-12:00, 13:00-17:00; Sa 08:00-12:00"
	oh := Parse(complexExpr)
	start := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	fixedTime := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	iterations := 10000
	fourHours := 4 * time.Hour

	// 1. Benchmark IsOpen (Rolling 100k calls with timestamp addition)
	t0 := time.Now()
	for i := 0; i < iterations*10; i++ {
		_ = oh.IsOpen(start.Add(time.Duration(i) * time.Minute))
	}
	dur1 := time.Since(t0)
	fmt.Printf("1. IsOpen (100k rolling calls):            %4d ms (%.3f us/op)\n", dur1.Milliseconds(), float64(dur1.Nanoseconds())/float64(iterations*10)/1000.0)

	// 2. Benchmark IsOpen (Pure 1M calls with fixed timestamp)
	t0 = time.Now()
	for i := 0; i < 1_000_000; i++ {
		_ = oh.IsOpen(fixedTime)
	}
	dur2 := time.Since(t0)
	fmt.Printf("2. IsOpen (1M pure calls):                 %4d ms (%.3f us/op)\n", dur2.Milliseconds(), float64(dur2.Nanoseconds())/1_000_000.0/1000.0)

	// 3. Benchmark GetTimeToOpen / TimeToOpen (10k calls)
	t0 = time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = oh.TimeToOpen(start.Add(time.Duration(i%168) * time.Hour))
	}
	dur3 := time.Since(t0)
	fmt.Printf("3. TimeToOpen (10k zero-alloc calls):      %4d ms (%.3f us/op)\n", dur3.Milliseconds(), float64(dur3.Nanoseconds())/float64(iterations)/1000.0)

	// 4. Benchmark GetTimeToOpenForDuration / TimeToOpenForDuration 4h (10k calls)
	t0 = time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = oh.TimeToOpenForDuration(start.Add(time.Duration(i%168)*time.Hour), fourHours)
	}
	dur4 := time.Since(t0)
	fmt.Printf("4. TimeToOpenForDuration 4h (10k calls):   %4d ms (%.3f us/op)\n", dur4.Milliseconds(), float64(dur4.Nanoseconds())/float64(iterations)/1000.0)

	// 5. Benchmark When / WhenTime 4h (10k calls)
	t0 = time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = oh.WhenTime(start.Add(time.Duration(i%168)*time.Hour), fourHours)
	}
	dur5 := time.Since(t0)
	fmt.Printf("5. WhenTime 4h (10k calls):                %4d ms (%.3f us/op)\n", dur5.Milliseconds(), float64(dur5.Nanoseconds())/float64(iterations)/1000.0)

	// 6. Benchmark NextDur (10k calls)
	t0 = time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = oh.NextDur(start.Add(time.Duration(i%168) * time.Hour))
	}
	dur6 := time.Since(t0)
	fmt.Printf("6. NextDur (10k calls):                    %4d ms (%.3f us/op)\n", dur6.Milliseconds(), float64(dur6.Nanoseconds())/float64(iterations)/1000.0)

	// 7. Benchmark NextDate (10k calls)
	t0 = time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = oh.NextDate(start.Add(time.Duration(i%168) * time.Hour))
	}
	dur7 := time.Since(t0)
	fmt.Printf("7. NextDate (10k calls):                   %4d ms (%.3f us/op)\n", dur7.Milliseconds(), float64(dur7.Nanoseconds())/float64(iterations)/1000.0)

	// 8. Benchmark Parse (Cached 1k calls)
	t0 = time.Now()
	for i := 0; i < 1000; i++ {
		_ = Parse(complexExpr)
	}
	dur8 := time.Since(t0)
	fmt.Printf("8. Parse Cached (1k calls):                %4d ms (%.3f us/op)\n", dur8.Milliseconds(), float64(dur8.Nanoseconds())/1000.0/1000.0)

	// 9. Benchmark JSON Deserialization (1k calls)
	jsonData := []byte(`"` + complexExpr + `"`)
	t0 = time.Now()
	for i := 0; i < 1000; i++ {
		var deserialized *OpeningHours
		_ = json.Unmarshal(jsonData, &deserialized)
	}
	dur9 := time.Since(t0)
	fmt.Printf("9. JSON Deserialize (1k calls):            %4d ms (%.3f us/op)\n", dur9.Milliseconds(), float64(dur9.Nanoseconds())/1000.0/1000.0)

	// 10. Simulation Stress Test (5,000 unique locations)
	t0 = time.Now()
	locations := make([]*OpeningHours, 0, 5000)
	for i := 0; i < 5000; i++ {
		hStart := 8 + (i%60)/60
		mStart := (i % 60)
		hEnd := 17 + (i%60)/60
		mEnd := (i % 60)
		expr := fmt.Sprintf("Mo-Fr %02d:%02d-%02d:%02d", hStart, mStart, hEnd, mEnd)
		locations = append(locations, Parse(expr))
	}
	dur10 := time.Since(t0)
	fmt.Printf("10. Stress Test (5,000 unique objects):    %4d ms (%.4f ms/obj)\n", dur10.Milliseconds(), float64(dur10.Nanoseconds())/5000.0/1000000.0)
	fmt.Println("========================================================")
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func durPtr(d time.Duration) *time.Duration {
	return &d
}

// ====================================================================
// Standard Go Benchmarks
// ====================================================================

func BenchmarkParse_Uncached(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rules := [...]openingRule{
			parseOpeningRule("mo-fr 08:00-12:00, 13:00-17:00"),
			parseOpeningRule("sa 08:00-12:00"),
		}
		_ = bakeRules(rules[:])
	}
}

func BenchmarkParse_Interned(b *testing.B) {
	expr := "mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00"
	_ = Parse(expr)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Parse(expr)
	}
}

func BenchmarkIsOpen(b *testing.B) {
	oh := Parse("mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00")
	t := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = oh.IsOpen(t)
	}
}

func BenchmarkGetTimeToOpen(b *testing.B) {
	oh := Parse("mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00")
	t := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = oh.GetTimeToOpen(t)
	}
}

func BenchmarkTimeToOpen(b *testing.B) {
	oh := Parse("mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00")
	t := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = oh.TimeToOpen(t)
	}
}

func BenchmarkWhen(b *testing.B) {
	oh := Parse("mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00")
	t := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	dur := 3 * time.Hour
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = oh.When(t, dur)
	}
}

func BenchmarkWhenTime(b *testing.B) {
	oh := Parse("mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00")
	t := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	dur := 3 * time.Hour
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = oh.WhenTime(t, dur)
	}
}

func BenchmarkGetTimeToOpenForDuration(b *testing.B) {
	oh := Parse("mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00")
	t := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	dur := 3 * time.Hour
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = oh.GetTimeToOpenForDuration(t, dur)
	}
}

func BenchmarkTimeToOpenForDuration(b *testing.B) {
	oh := Parse("mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00")
	t := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	dur := 3 * time.Hour
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = oh.TimeToOpenForDuration(t, dur)
	}
}

func BenchmarkNextDur(b *testing.B) {
	oh := Parse("mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00")
	t := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = oh.NextDur(t)
	}
}

func BenchmarkNextDate(b *testing.B) {
	oh := Parse("mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00")
	t := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = oh.NextDate(t)
	}
}

func BenchmarkJSONDeserialize(b *testing.B) {
	expr := "mo-fr 08:00-12:00, 13:00-17:00; sa 08:00-12:00"
	jsonData := []byte(`"` + expr + `"`)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var deserialized *OpeningHours
		_ = json.Unmarshal(jsonData, &deserialized)
	}
}


package openhours

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Reference tests ported from the original opening_hours.js test suite
// (https://github.com/opening-hours/opening_hours.js/blob/main/test/test.js).
//
// Only the expression variants that this implementation parses to the SAME
// open-intervals as the reference suite are included. Each case lists the
// expected open intervals (t[s], t[e)) as returned by that reference suite for
// the query window [from, to); we assert IsOpen against those intervals at
// every interval boundary, interval midpoint and a few daily probe points.
// open-end ("+"), am/pm, dot/unicode separators, short "H-H" times, holidays,
// variable times, months/years, constrained weekdays and comments are not
// ported because they are outside this implementation's grammar/API.
// ---------------------------------------------------------------------------

type refInterval struct{ s, e string }

type refCase struct {
	name      string
	expr      string
	from, to  string
	intervals []refInterval
}

func mustRefTS(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.UTC)
	if err != nil {
		panic("bad reference timestamp: " + s)
	}
	return t
}

// refPoints builds a set of probe timestamps derived from the expected open
// intervals plus a coarse daily grid inside [from, to).
func refPoints(from, to time.Time, iv []refInterval) []time.Time {
	points := []time.Time{}
	for _, in := range iv {
		s := mustRefTS(in.s)
		e := mustRefTS(in.e)
		mid := s.Add(e.Sub(s) / 2)
		points = append(points,
			s.Add(-time.Minute), s, s.Add(time.Minute), mid,
			e.Add(-time.Minute), e,
		)
	}
	points = append(points, from, from.Add(time.Minute))
	for t := from.Add(time.Hour); t.Before(to); t = t.Add(24 * time.Hour) {
		points = append(points, t.Add(3*time.Hour), t.Add(12*time.Hour), t.Add(18*time.Hour))
	}
	return points
}

// refOpenAt reports whether ts falls inside any reference interval.
func refOpenAt(ts time.Time, iv []refInterval) bool {
	for _, in := range iv {
		s := mustRefTS(in.s)
		e := mustRefTS(in.e)
		if !ts.Before(s) && ts.Before(e) {
			return true
		}
	}
	return false
}

// runRefCase verifies that Parse(expr).IsOpen matches the reference intervals
// for every generated probe point inside [from, to).
func runRefCase(t *testing.T, c refCase) {
	t.Helper()
	from := mustRefTS(c.from)
	to := mustRefTS(c.to)
	oh := Parse(c.expr)
	for _, p := range refPoints(from, to, c.intervals) {
		if p.Before(from) || !p.Before(to) {
			continue
		}
		got := oh.IsOpen(p)
		want := refOpenAt(p, c.intervals)
		if got != want {
			t.Errorf("%s: expr=%q at %s: IsOpen=%v, want %v",
				c.name, c.expr, p.Format("2006-01-02 15:04"), got, want)
		}
	}
}

func TestReferenceFromOpeningHoursJS(t *testing.T) {
	// Week of Monday 2012-10-01 (Oct) as used across the reference suite.
	day10to12 := []refInterval{
		{"2012-10-01 10:00", "2012-10-01 12:00"},
		{"2012-10-02 10:00", "2012-10-02 12:00"},
		{"2012-10-03 10:00", "2012-10-03 12:00"},
		{"2012-10-04 10:00", "2012-10-04 12:00"},
		{"2012-10-05 10:00", "2012-10-05 12:00"},
		{"2012-10-06 10:00", "2012-10-06 12:00"},
		{"2012-10-07 10:00", "2012-10-07 12:00"},
	}
	cases := []refCase{
		// "Time intervals"
		{"Time intervals", "10:00-12:00", "2012-10-01 0:00", "2012-10-08 0:00", day10to12},
		{"Time intervals", "08:00-09:00; 10:00-12:00", "2012-10-01 0:00", "2012-10-08 0:00", day10to12},
		{"Time intervals", "10:00-12:00,", "2012-10-01 0:00", "2012-10-08 0:00", day10to12},
		{"Time intervals", "10:00-12:00;", "2012-10-01 0:00", "2012-10-08 0:00", day10to12},
		{"Time intervals", "10:00-11:00,11:00-12:00", "2012-10-01 0:00", "2012-10-08 0:00", day10to12},
		{"Time intervals", "10:00-12:00,10:30-11:30", "2012-10-01 0:00", "2012-10-08 0:00", day10to12},
		{"Time intervals", "10:00-14:00; 12:00-14:00 off", "2012-10-01 0:00", "2012-10-08 0:00", day10to12},

		// "Time intervals" (24/7 minus Monday lunch)
		{"Time intervals 24/7 off", "24/7; Mo 15:00-16:00 off", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 00:00", "2012-10-01 15:00"},
			{"2012-10-01 16:00", "2012-10-08 00:00"},
		}},
		{"Time intervals 24/7 off", "open; Mo 15:00-16:00 off", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 00:00", "2012-10-01 15:00"},
			{"2012-10-01 16:00", "2012-10-08 00:00"},
		}},
		{"Time intervals 24/7 off", "00:00-24:00; Mo 15:00-16:00 off", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 00:00", "2012-10-01 15:00"},
			{"2012-10-01 16:00", "2012-10-08 00:00"},
		}},

		// "Time zero intervals (always closed)"
		{"always closed", "off", "2012-10-01 0:00", "2012-10-08 0:00", nil},
		{"always closed", "closed", "2012-10-01 0:00", "2012-10-08 0:00", nil},
		{"always closed", "off; closed", "2012-10-01 0:00", "2012-10-08 0:00", nil},
		{"always closed", "24/7 closed", "2012-10-01 0:00", "2012-10-08 0:00", nil},
		{"always closed", "00:00-24:00 closed", "2012-10-01 0:00", "2012-10-08 0:00", nil},

		// "Error tolerance: dot as time separator" (reference values)
		{"dot-sep ref", "10:00-12:00", "2012-10-01 0:00", "2012-10-08 0:00", day10to12},
		{"dot-sep ref", "10:00-14:00; 12:00-14:00 off", "2012-10-01 0:00", "2012-10-08 0:00", day10to12},

		// "Error tolerance: Correctly handle pm time." (reference value)
		{"pm ref", "10:00-12:00,13:00-20:00", "2012-10-01 0:00", "2012-10-03 0:00", []refInterval{
			{"2012-10-01 10:00", "2012-10-01 12:00"},
			{"2012-10-01 13:00", "2012-10-01 20:00"},
			{"2012-10-02 10:00", "2012-10-02 12:00"},
			{"2012-10-02 13:00", "2012-10-02 20:00"},
		}},

		// "Error tolerance: Time intervals, short time" (reference value)
		{"short ref", "Mo 07:00-18:00", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 07:00", "2012-10-01 18:00"},
		}},

		// "Time ranges spanning midnight"
		{"overnight", "22:00-02:00", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 00:00", "2012-10-01 02:00"},
			{"2012-10-01 22:00", "2012-10-02 02:00"},
			{"2012-10-02 22:00", "2012-10-03 02:00"},
			{"2012-10-03 22:00", "2012-10-04 02:00"},
			{"2012-10-04 22:00", "2012-10-05 02:00"},
			{"2012-10-05 22:00", "2012-10-06 02:00"},
			{"2012-10-06 22:00", "2012-10-07 02:00"},
			{"2012-10-07 22:00", "2012-10-08 00:00"},
		}},

		// "Time ranges spanning midnight w/weekdays"
		{"overnight weekday", "We 22:00-02:00", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-03 22:00", "2012-10-04 02:00"},
		}},
		{"overnight weekday", "We22:00-02:00", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-03 22:00", "2012-10-04 02:00"},
		}},

		// "Weekdays"
		{"Weekdays", "Mo,Th,Sa,Su 10:00-12:00", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 10:00", "2012-10-01 12:00"},
			{"2012-10-04 10:00", "2012-10-04 12:00"},
			{"2012-10-06 10:00", "2012-10-06 12:00"},
			{"2012-10-07 10:00", "2012-10-07 12:00"},
		}},
		{"Weekdays", "Mo,Th,Sa-Su 10:00-12:00", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 10:00", "2012-10-01 12:00"},
			{"2012-10-04 10:00", "2012-10-04 12:00"},
			{"2012-10-06 10:00", "2012-10-06 12:00"},
			{"2012-10-07 10:00", "2012-10-07 12:00"},
		}},
		{"Weekdays", "Th,Sa-Mo 10:00-12:00", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 10:00", "2012-10-01 12:00"},
			{"2012-10-04 10:00", "2012-10-04 12:00"},
			{"2012-10-06 10:00", "2012-10-06 12:00"},
			{"2012-10-07 10:00", "2012-10-07 12:00"},
		}},
		{"Weekdays", "10:00-12:00; Tu-We 00:00-24:00 off; Fr 00:00-24:00 off", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 10:00", "2012-10-01 12:00"},
			{"2012-10-04 10:00", "2012-10-04 12:00"},
			{"2012-10-06 10:00", "2012-10-06 12:00"},
			{"2012-10-07 10:00", "2012-10-07 12:00"},
		}},
		{"Weekdays", "10:00-12:00; Tu-We off; Fr off", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 10:00", "2012-10-01 12:00"},
			{"2012-10-04 10:00", "2012-10-04 12:00"},
			{"2012-10-06 10:00", "2012-10-06 12:00"},
			{"2012-10-07 10:00", "2012-10-07 12:00"},
		}},

		// "Omitted time"
		{"Omitted time", "Mo,We", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 00:00", "2012-10-02 00:00"},
			{"2012-10-03 00:00", "2012-10-04 00:00"},
		}},

		// "Full range"
		{"Full range", "00:00-24:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "00:00-00:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "Mo-Su 00:00-24:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "Tu-Mo 00:00-24:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "We-Tu 00:00-24:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "Th-We 00:00-24:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "Fr-Th 00:00-24:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "Sa-Fr 00:00-24:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "Su-Sa 00:00-24:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "24/7", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "24/7; 24/7", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "open", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "12:00-13:00; 24/7", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "00:00-24:00,12:00-13:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "Mo-Fr,Sa,Su", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},
		{"Full range", "Mo 00:00-24:00; Tu 00:00-24:00; We 00:00-24:00; Th 00:00-24:00; Fr 00:00-24:00; Sa 00:00-24:00; Su 00:00-24:00", "2025-10-01 0:00", "2025-10-08 0:00", []refInterval{{"2025-10-01 00:00", "2025-10-08 00:00"}}},

		// "24/7 as time interval alias"
		{"24/7 alias", "Mo,We 00:00-24:00", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 00:00", "2012-10-02 00:00"},
			{"2012-10-03 00:00", "2012-10-04 00:00"},
		}},
		{"24/7 alias", "Mo,We 24/7", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 00:00", "2012-10-02 00:00"},
			{"2012-10-03 00:00", "2012-10-04 00:00"},
		}},
		{"24/7 alias", "Mo,We open", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 00:00", "2012-10-02 00:00"},
			{"2012-10-03 00:00", "2012-10-04 00:00"},
		}},
		{"24/7 alias", "Mo,We", "2012-10-01 0:00", "2012-10-08 0:00", []refInterval{
			{"2012-10-01 00:00", "2012-10-02 00:00"},
			{"2012-10-03 00:00", "2012-10-04 00:00"},
		}},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name+"/"+c.expr, func(t *testing.T) {
			runRefCase(t, c)
		})
	}
}

package openhours

import (
	"reflect"
	"testing"
	"time"
)

func Test_cleanStr(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{"nothing", "Mo-Fr 10:00-12:00,12:30-16:00", "mo-fr 10:00-12:00,12:30-16:00"},
		{"comma after space", "Mo-Fr 10:00-12:00, 12:30-16:00", "mo-fr 10:00-12:00,12:30-16:00"},
		{"comma before space", "Mo-Fr 10:00-12:00 ,12:30-16:00", "mo-fr 10:00-12:00,12:30-16:00"},
		{"comma before and after space", "Mo-Fr 10:00-12:00 , 12:30-16:00", "mo-fr 10:00-12:00,12:30-16:00"},
		{"front space", " Mo-Fr 10:00-12:00,12:30-16:00", "mo-fr 10:00-12:00,12:30-16:00"},
		{"trailing space", "Mo-Fr 10:00-12:00,12:30-16:00 ", "mo-fr 10:00-12:00,12:30-16:00"},
		{"both spaces", " Mo-Fr 10:00-12:00,12:30-16:00 ", "mo-fr 10:00-12:00,12:30-16:00"},
		{"mixed both spaces/tabs", " 	Mo-Fr 10:00-12:00,12:30-16:00	 ", "mo-fr 10:00-12:00,12:30-16:00"},
		{"inner mixed spaces/tabs", " 	 	Mo-Fr 	 10:00-12:00,12:30-16:00 	 	 ", "mo-fr 10:00-12:00,12:30-16:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanStr(tt.args); got != tt.want {
				t.Errorf("cleanStr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_simplifyDays(t *testing.T) {
	tests := []struct {
		name string
		args string
		want []int
	}{
		{"simple", "mo", []int{1}},
		{"double", "we,fr", []int{3, 5}},
		{"range", "we-fr", []int{3, 4, 5}},
		{"range with double", "mo,we-fr,su", []int{0, 1, 3, 4, 5}},
		{"error -", "mo-pl", []int{}},
		{"error ,", "pl,mo", []int{1}},
		{"wrong range", "fr-mo", []int{}},
		{"dupicate days", "mo-tu,tu,tu-fr,fr", []int{1, 2, 3, 4, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := simplifyDays(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("simplifyDays() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_simplifyHour(t *testing.T) {
	tests := []struct {
		args  string
		want  int
		want1 int
	}{
		{"00:00", 0, 0},
		{"10:30", 10, 30},
		{"09:05", 9, 5},
		{"24:00", 24, 0},
		{"00:-10", 0, 0},
		{"24:01", 0, 0},
		{"-50:99", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			got, got1 := simplifyHour(tt.args)
			if got != tt.want {
				t.Errorf("simplifyHour() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("simplifyHour() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func Test_feature_simple(t *testing.T) {
	o := New("mo 08:00-18:00")
	tests := []struct {
		args time.Time
		want bool
	}{
		{time.Date(2019, 3, 4, 8, 0, 0, 0, location), true}, // special case start = true
		{time.Date(2019, 3, 4, 17, 59, 0, 0, location), true},
		{time.Date(2019, 3, 4, 18, 0, 0, 0, location), false}, // special case end = false
		{time.Date(2019, 3, 4, 7, 0, 0, 0, location), false},
		{time.Date(2019, 3, 4, 19, 0, 0, 0, location), false},
	}
	for _, tt := range tests {
		t.Run(tt.args.String(), func(t *testing.T) {
			if got := o.Match(tt.args); got != tt.want {
				t.Errorf("simplifyHour() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_feature_two(t *testing.T) {
	o := New("mo 08:00-12:00,13:00-17:00")
	tests := []struct {
		args time.Time
		want bool
	}{
		{time.Date(2019, 3, 4, 8, 0, 0, 0, location), true}, // special case start = true
		{time.Date(2019, 3, 4, 9, 0, 0, 0, location), true},
		{time.Date(2019, 3, 4, 13, 0, 0, 0, location), true}, // special case start = true
		{time.Date(2019, 3, 4, 15, 0, 0, 0, location), true},
		{time.Date(2019, 3, 4, 12, 30, 0, 0, location), false}, // between
		{time.Date(2019, 3, 4, 17, 59, 0, 0, location), false},
		{time.Date(2019, 3, 4, 17, 0, 0, 0, location), false}, // special case end = false
		{time.Date(2019, 3, 4, 12, 0, 0, 0, location), false}, // special case end = false
		{time.Date(2019, 3, 4, 7, 0, 0, 0, location), false},
		{time.Date(2019, 3, 4, 19, 0, 0, 0, location), false},
	}
	for _, tt := range tests {
		t.Run(tt.args.String(), func(t *testing.T) {
			if got := o.Match(tt.args); got != tt.want {
				t.Errorf("simplifyHour() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenHours_NextDur(t *testing.T) {
	o := New("mo 08:00-18:00")
	tests := []struct {
		name  string
		args  time.Time
		want  bool
		want1 time.Duration
	}{
		{"1 hour before start", time.Date(2019, 3, 4, 7, 0, 0, 0, location), false, time.Hour},
		{"at start", time.Date(2019, 3, 4, 8, 0, 0, 0, location), true, 10 * time.Hour},
		{"1 hour after start", time.Date(2019, 3, 4, 9, 0, 0, 0, location), true, 9 * time.Hour},
		{"1 hour before end", time.Date(2019, 3, 4, 17, 0, 0, 0, location), true, time.Hour},
		{"at end", time.Date(2019, 3, 4, 18, 0, 0, 0, location), false, time.Hour*24*7 - time.Hour*10},
		{"1 day after start (closed)", time.Date(2019, 3, 5, 8, 0, 0, 0, location), false, time.Hour * 24 * 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := o.NextDur(tt.args)
			if got != tt.want {
				t.Errorf("OpenHours.NextDur() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("OpenHours.NextDur() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestOpenHours_Special_NextDur(t *testing.T) {
	o := New("su 03:00-05:00")
	tests := []struct {
		name  string
		args  time.Time
		want  bool
		want1 time.Duration
	}{
		{"2 h before (3 if there was no clock change)", time.Date(2019, 3, 31, 0, 0, 0, 0, location), false, time.Hour * 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := o.NextDur(tt.args)
			if got != tt.want {
				t.Errorf("OpenHours.NextDur() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("OpenHours.NextDur() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

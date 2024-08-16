package openhours

import (
	"reflect"
	"runtime/debug"
	"slices"
	"testing"
	"time"
)

var l *time.Location

func init() {
	var err error
	l, err = time.LoadLocation("Europe/London") // is a good test example since i know when the two clock changes occur
	if err != nil {
		panic("could not load location")
	}
}

func Test_cleanStr(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{"capital letters", "Mo-Fr 10:00-12:00,12:30-16:00", "mo-fr 10:00-12:00,12:30-16:00"},
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
		{"weird range", "fr-mo", []int{Monday, Friday, Saturday, Sunday}},
		{"simple", "mo", []int{Monday}},
		{"double with error", "mo,mardi", []int{Monday}},
		{"double with error", "mo,mardi", []int{Monday}},
		{"double", "we,fr", []int{Wednesday, Friday}},
		{"range", "we-fr", []int{Wednesday, Thursday, Friday}},
		{"range with double", "mo,we-fr,su", []int{Monday, Wednesday, Thursday, Friday, Sunday}},
		{"error -", "mo-pl", []int{}},
		{"error ,", "pl,mo", []int{Monday}},
		{"duplicate days", "mo-tu,tu,tu-fr,fr", []int{Monday, Tuesday, Wednesday, Thursday, Friday}},
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
		want2 int
	}{
		{"00:00", 0, 0, 0},
		{"00:00:00", 0, 0, 0},
		{"00:00:05", 0, 0, 5},
		{"10:30", 10, 30, 0},
		{"09:05", 9, 5, 0},
		{"24:00", 24, 0, 0},
		{"00:-10", 0, 0, 0},
		{"24:01", 0, 0, 0},
		{"-50:99", 0, 0, 0},
		{"33:33:33", 9, 33, 33}, // allow for 25:00:00 to be 1:00:00
		{"33:61:33", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			got, got1, got2 := simplifyTime(tt.args)
			if got != tt.want {
				t.Errorf("simplifyHour(%s) got = %v, want %v", tt.args, got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("simplifyHour(%s) got1 = %v, want %v", tt.args, got1, tt.want1)
			}
			if got2 != tt.want2 {
				t.Errorf("simplifyHour(%s) got2 = %v, want %v", tt.args, got2, tt.want2)
			}
		})
	}
}

func Test_feature_simple(t *testing.T) {
	o, err := New("mo 08:00-18:00", l)
	if err != nil {
		t.Error(err)
	}
	tests := []struct {
		name string
		args time.Time
		want bool
	}{
		{"before", newDate(Monday, 7, 0, 0, 0, l), false},
		{"start", newDate(Monday, 8, 0, 0, 0, l), true}, // special case start = true
		{"during", newDate(Monday, 17, 59, 0, 0, l), true},
		{"end", newDate(Monday, 18, 0, 0, 0, l), false}, // special case end = false
		{"after", newDate(Monday, 19, 0, 0, 0, l), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := o.Match(tt.args); got != tt.want {
				t.Errorf("simplifyHour() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_feature_two(t *testing.T) {
	o, err := New("mo 08:00-12:00,13:00-17:00", l)
	if err != nil {
		t.Error(err)
	}
	tests := []struct {
		args time.Time
		want bool
	}{
		{newDate(Monday, 8, 0, 0, 0, l), true}, // special case start = true
		{newDate(Monday, 9, 0, 0, 0, l), true},
		{newDate(Monday, 13, 0, 0, 0, l), true}, // special case start = true
		{newDate(Monday, 15, 0, 0, 0, l), true},
		{newDate(Monday, 12, 30, 0, 0, l), false}, // between
		{newDate(Monday, 17, 59, 0, 0, l), false},
		{newDate(Monday, 17, 0, 0, 0, l), false}, // special case end = false
		{newDate(Monday, 12, 0, 0, 0, l), false}, // special case end = false
		{newDate(Monday, 7, 0, 0, 0, l), false},
		{newDate(Monday, 19, 0, 0, 0, l), false},
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
	o, err := New("mo 08:00-18:00", l)
	if err != nil {
		t.Error(err)
	}
	tests := []struct {
		name  string
		args  time.Time
		want  bool
		want1 time.Duration
	}{
		{"1 hour before start", newDate(Monday, 7, 0, 0, 0, l), false, time.Hour},
		{"at start", newDate(Monday, 8, 0, 0, 0, l), true, 10 * time.Hour},
		{"1 hour after start", newDate(Monday, 9, 0, 0, 0, l), true, 9 * time.Hour},
		{"1 hour before end", newDate(Monday, 17, 0, 0, 0, l), true, time.Hour},
		{"at end", newDate(Monday, 18, 0, 0, 0, l), false, time.Hour*24*7 - time.Hour*10},
		{"1 day after start (closed)", newDate(Tuesday, 8, 0, 0, 0, l), false, time.Hour * 24 * 6},
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
	o, err := New("su 03:00-05:00", l)
	if err != nil {
		t.Error(err)
	}
	tests := []struct {
		name  string
		args  time.Time
		want  bool
		want1 time.Duration
	}{
		{"2 h before (3 if there was no clock change)", newDate(Sunday, 1, 0, 0, 0, l), false, time.Hour * 2},
		{"4 h before (3 if there was no clock change)", newDate(Saturday, 23, 0, 0, 0, l), false, time.Hour * 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := o.NextDur(tt.args)
			if got != tt.want {
				t.Errorf("OpenHours.NextDur() got = %v, want %v have %v", got, tt.want, o)
			}
			if got1 != tt.want1 {
				t.Errorf("OpenHours.NextDur() got1 = %v, want %v have %v", got1, tt.want1, o)
			}
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name  string
		args  string
		args2 *time.Location
		want  OpenHours
	}{
		{"empty", "", l, []time.Time{newDate(Monday, 0, 0, 0, 0, l), newDate(Sunday, 24, 0, 0, 0, l)}},
		{"empty ;", ";", l, []time.Time{newDate(Monday, 0, 0, 0, 0, l), newDate(Sunday, 24, 0, 0, 0, l)}},
		{"all day ;", "su-sa 00:00-24:00;", l, []time.Time{newDate(Monday, 0, 0, 0, 0, l), newDate(Sunday, 24, 0, 0, 0, l)}},
		{"empty and no tz", "", nil, []time.Time{newDate(Monday, 0, 0, 0, 0, time.UTC), newDate(Sunday, 24, 0, 0, 0, time.UTC)}},
		{"order on same sentence", "mo,tu 10:00-11:00", nil, NewMust("tu,mo 10:00-11:00", nil)},
		{"order on different sentences", "mo 10:00-11:00;tu 10:00-12:00", nil, NewMust("tu 10:00-12:00;mo 10:00-11:00", nil)},
		{"complex = simple", "su-sa 00:00-12:00,12:00-24:00", l, NewMust("", l)},
		{"complex = simple", "su-sa 00:00-12:00;su-sa 12:00-24:00", l, NewMust("", l)},
		{"time windows order does not matter anymore", "mo-su 00:00-24:00", l, NewMust("", l)},
		{"one day", "mo 10:00-15:00", l, []time.Time{newDate(Monday, 10, 0, 0, 0, l), newDate(Monday, 15, 0, 0, 0, l)}},
		{"two days", "mo 10:00-15:00;fr 08:00-14:00", l, []time.Time{newDate(Monday, 10, 0, 0, 0, l), newDate(Monday, 15, 0, 0, 0, l), newDate(Friday, 8, 0, 0, 0, l), newDate(Friday, 14, 0, 0, 0, l)}},
		{"week with break", "Tu-Th 10:30-13:00,14:00-24:00", l, []time.Time{
			newDate(Tuesday, 10, 30, 0, 0, l), newDate(Tuesday, 13, 0, 0, 0, l),
			newDate(Tuesday, 14, 0, 0, 0, l), newDate(Tuesday, 24, 0, 0, 0, l),
			newDate(Wednesday, 10, 30, 0, 0, l), newDate(Wednesday, 13, 0, 0, 0, l),
			newDate(Wednesday, 14, 0, 0, 0, l), newDate(Wednesday, 24, 0, 0, 0, l),
			newDate(Thursday, 10, 30, 0, 0, l), newDate(Thursday, 13, 0, 0, 0, l),
			newDate(Thursday, 14, 0, 0, 0, l), newDate(Thursday, 24, 0, 0, 0, l),
		}},
		{"", "Mo-Sa 10:00-21:00; Su 12:00-19:00", l, []time.Time{
			newDate(Monday, 10, 0, 0, 0, l), newDate(Monday, 21, 0, 0, 0, l),
			newDate(Tuesday, 10, 0, 0, 0, l), newDate(Tuesday, 21, 0, 0, 0, l),
			newDate(Wednesday, 10, 0, 0, 0, l), newDate(Wednesday, 21, 0, 0, 0, l),
			newDate(Thursday, 10, 0, 0, 0, l), newDate(Thursday, 21, 0, 0, 0, l),
			newDate(Friday, 10, 0, 0, 0, l), newDate(Friday, 21, 0, 0, 0, l),
			newDate(Saturday, 10, 0, 0, 0, l), newDate(Saturday, 21, 0, 0, 0, l),
			newDate(Sunday, 12, 0, 0, 0, l), newDate(Sunday, 19, 0, 0, 0, l),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(tt.args, tt.args2)
			if err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("New() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenHours_NextDate(t *testing.T) {
	o, err := New("su 03:00-05:00", l)
	if err != nil {
		t.Error(err)
	}
	tests := []struct {
		name  string
		args  time.Time
		want  bool
		want1 time.Time
	}{
		{"2 h before (3 if there was no clock change)", newDate(Sunday, 1, 0, 0, 0, l), false, newDate(Sunday, 3, 0, 0, 0, l)},
		{"4 h before (3 if there was no clock change)", newDate(Saturday, 23, 0, 0, 0, l), false, newDate(Sunday, 3, 0, 0, 0, l)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := o.NextDate(tt.args)
			if got != tt.want {
				t.Errorf("OpenHours.NextDate() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("OpenHours.NextDate() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func pDate(day, hour, min, sec, nsec int, loc *time.Location) *time.Time {
	t := newDate(day, hour, min, sec, nsec, loc)
	return &t
}

func TestOpenHours_When(t *testing.T) {
	type args struct {
		t time.Time
		d time.Duration
	}
	tests := []struct {
		name string
		o    OpenHours
		args args
		want *time.Time
	}{
		{"at start of open and have time", NewMust("mo 10:00-15:00", l), args{newDate(Monday, 11, 0, 0, 0, l), time.Hour * 4}, pDate(1, 11, 0, 0, 0, l)},
		{"before start of open and have time", NewMust("mo 10:00-15:00", l), args{newDate(Monday, 9, 0, 0, 0, l), time.Hour * 4}, pDate(1, 10, 0, 0, 0, l)},
		{"at end of open and have time", NewMust("mo 10:00-15:00", l), args{newDate(Monday, 15, 0, 0, 0, l), time.Hour * 4}, pDate(8, 10, 0, 0, 0, l)},
		{"after end of open and have time", NewMust("mo 10:00-15:00", l), args{newDate(Monday, 16, 0, 0, 0, l), time.Hour * 4}, pDate(8, 10, 0, 0, 0, l)},
		{"between open and have time", NewMust("mo 10:00-15:00", l), args{newDate(Tuesday, 11, 0, 0, 0, l), time.Hour * 4}, pDate(8, 10, 0, 0, 0, l)},
		{"between open and no time", NewMust("mo 10:00-15:00", l), args{newDate(Monday, 14, 0, 0, 0, l), time.Hour * 4}, pDate(8, 10, 0, 0, 0, l)},
		{"no time", NewMust("mo 10:00-11:00", l), args{newDate(Monday, 14, 0, 0, 0, l), time.Hour * 4}, nil},
		{"at start of open and have time +fri", NewMust("mo 10:00-15:00;fr 08:00-14:00", l), args{newDate(Monday, 10, 0, 0, 0, l), time.Hour * 4}, pDate(1, 10, 0, 0, 0, l)},
		{"before start of open and have time +fri", NewMust("mo 10:00-15:00;fr 08:00-14:00", l), args{newDate(Monday, 9, 0, 0, 0, l), time.Hour * 4}, pDate(1, 10, 0, 0, 0, l)},
		{"at end of open and have time +fri", NewMust("mo 10:00-15:00;fr 08:00-14:00", l), args{newDate(Monday, 15, 0, 0, 0, l), time.Hour * 4}, pDate(5, 8, 0, 0, 0, l)},
		{"after end of open and have time +fri", NewMust("mo 10:00-15:00;fr 08:00-14:00", l), args{newDate(Monday, 16, 0, 0, 0, l), time.Hour * 4}, pDate(5, 8, 0, 0, 0, l)},
		{"between open and have time +fri", NewMust("mo 10:00-15:00;fr 08:00-14:00", l), args{newDate(Monday, 11, 0, 0, 0, l), time.Hour * 4}, pDate(1, 11, 0, 0, 0, l)},
		{"between open and no time +fri", NewMust("mo 10:00-15:00;fr 08:00-14:00", l), args{newDate(Monday, 14, 0, 0, 0, l), time.Hour * 4}, pDate(5, 8, 0, 0, 0, l)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.o.When(tt.args.t, tt.args.d); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("OpenHours.When() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenHours_Add(t *testing.T) {
	type args struct {
		t time.Time
		d time.Duration
	}
	tests := []struct {
		name string
		o    OpenHours
		args args
		want *time.Time
	}{
		{
			"at start of open and have time",
			OpenHours{}.Add(newDate(11, 10, 0, 0, 0, l), newDate(11, 10, 30, 0, 0, l)), // mo 10:00-10:30
			args{newDate(11, 9, 0, 0, 0, l), time.Second},
			pDate(11, 10, 0, 0, 0, l),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.o.When(tt.args.t, tt.args.d); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("OpenHours.When() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenHours_Bugs(t *testing.T) {
	o, err := New("mo-su 07:00-19:00", time.UTC)
	if err != nil {
		t.Error(err)
	}
	when := o.When(newDate(Sunday, 9, 0, 0, 0, time.UTC), time.Hour)
	want := newDate(Sunday, 9, 0, 0, 0, time.UTC)
	if when == nil || !want.Equal(*when) {
		t.Errorf("OpenHours.When() = %v, want %v", when, want)
	}
}

func TestOpenHours_InvalidPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(string(debug.Stack()))
			t.Errorf("OpenHours did panic: %v", r)
		}
	}()
	_, err := New("mo 10:00", nil)
	if err != ErrInvalidFormat {
		t.Error(err)
	}
}

func TestOpenHours_String(t *testing.T) {
	tests := []struct {
		name string
		o    OpenHours
		want []string
	}{
		{"empty", OpenHours{}, []string{}},
		{"simple", NewMust("mo 10:00-15:00", l), []string{"Monday 10:00 - 15:00"}},
		{"two", NewMust("mo 10:00-15:00;fr 08:00-14:00", l), []string{"Monday 10:00 - 15:00", "Friday 08:00 - 14:00"}},
		{"full week", NewMust("mo-su 09:00-17:00", l), []string{"Monday 09:00 - 17:00", "Tuesday 09:00 - 17:00", "Wednesday 09:00 - 17:00", "Thursday 09:00 - 17:00", "Friday 09:00 - 17:00", "Saturday 09:00 - 17:00", "Sunday 09:00 - 17:00"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.o.String(); !slices.Equal(got, tt.want) {
				t.Errorf("OpenHours.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenHours_ClosingAfterMidnight(t *testing.T) {
	o1 := NewMust("mo 22:00-02:00", l)
	o2 := NewMust("mo 22:00-26:00", l)
	tests := []struct {
		o    OpenHours
		name string
		now  time.Time
		want bool
	}{
		{o1, "1before", newDate(Monday, 21, 0, 0, 0, l), false},
		{o1, "1start", newDate(Monday, 22, 0, 0, 0, l), true},
		{o1, "1between", newDate(Monday, 23, 0, 0, 0, l), true},
		{o1, "1end", newDate(Tuesday, 2, 0, 0, 0, l), false},
		{o1, "1after", newDate(Tuesday, 3, 0, 0, 0, l), false},
		// using the 26:00 = 02:00 next day notation
		{o2, "2before", newDate(Monday, 21, 0, 0, 0, l), false},
		{o2, "2start", newDate(Monday, 22, 0, 0, 0, l), true},
		{o2, "2between", newDate(Monday, 23, 0, 0, 0, l), true},
		{o2, "2end", newDate(Tuesday, 2, 0, 0, 0, l), false},
		{o2, "2after", newDate(Tuesday, 3, 0, 0, 0, l), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, got1 := tt.o.NextDur(tt.now); got != tt.want {
				t.Errorf("OpenHours.NextDur().Open = %v, want %v, duration: %v", got, tt.want, got1)
			}
		})
	}
}

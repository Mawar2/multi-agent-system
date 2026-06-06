package hunter

import (
	"testing"
	"time"
)

func TestDeadlineTime_RFC3339(t *testing.T) {
	opp := &Opportunity{ResponseDeadline: "2026-07-01T17:00:00-05:00"}
	dt, ok := opp.DeadlineTime()
	if !ok {
		t.Fatal("want ok=true")
	}
	if dt.IsZero() {
		t.Error("want non-zero time")
	}
}

func TestDeadlineTime_DateOnly(t *testing.T) {
	opp := &Opportunity{ResponseDeadline: "2026-07-01"}
	_, ok := opp.DeadlineTime()
	if !ok {
		t.Fatal("want ok=true for YYYY-MM-DD format")
	}
}

func TestDeadlineTime_Empty(t *testing.T) {
	opp := &Opportunity{}
	_, ok := opp.DeadlineTime()
	if ok {
		t.Error("want ok=false for empty deadline")
	}
}

func TestDeadlineTime_Invalid(t *testing.T) {
	opp := &Opportunity{ResponseDeadline: "not-a-date"}
	_, ok := opp.DeadlineTime()
	if ok {
		t.Error("want ok=false for unparseable deadline")
	}
}

func TestDaysUntilDeadline_PastDate(t *testing.T) {
	opp := &Opportunity{ResponseDeadline: "2020-01-01"}
	if d := opp.DaysUntilDeadline(); d >= 0 {
		t.Errorf("want negative days for past deadline, got %d", d)
	}
}

func TestDaysUntilDeadline_FutureDate(t *testing.T) {
	future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	opp := &Opportunity{ResponseDeadline: future}
	d := opp.DaysUntilDeadline()
	// Allow ±1 for day boundary crossing
	if d < 29 || d > 31 {
		t.Errorf("want ~30 days, got %d", d)
	}
}

func TestDaysUntilDeadline_Empty(t *testing.T) {
	opp := &Opportunity{}
	if d := opp.DaysUntilDeadline(); d != -1 {
		t.Errorf("want -1 for empty deadline, got %d", d)
	}
}

func TestIsSmallBusinessSetAside(t *testing.T) {
	tests := []struct {
		setAside string
		want     bool
	}{
		{"Small Business", true},
		{"8(a)", true},
		{"HUBZone", true},
		{"WOSB", true},
		{"", false},
		{"N/A", false},
	}
	for _, tc := range tests {
		opp := &Opportunity{TypeOfSetAside: tc.setAside}
		if got := opp.IsSmallBusinessSetAside(); got != tc.want {
			t.Errorf("TypeOfSetAside=%q: want %v, got %v", tc.setAside, tc.want, got)
		}
	}
}

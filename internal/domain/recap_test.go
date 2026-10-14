package domain

import (
	"reflect"
	"testing"
	"time"
)

func fixtureNights() []NightRow {
	a := func(id int64, name string, st AttendanceStatus, fine int) AttendanceRow {
		return AttendanceRow{MemberID: id, DisplayName: name, Status: st, Fine: fine}
	}
	return []NightRow{
		{Date{2026, 9, 15}, NightNoReport, "", []AttendanceRow{
			a(4, "Gemilang (D-1)", Absent, 5000), a(5, "E-10", Absent, 5000)}},
		{Date{2026, 9, 7}, NightReported, "Aman.", []AttendanceRow{
			a(1, "Dhani (A-2)", Present, 0), a(2, "Yusuf (B-3)", Present, 0), a(3, "C-10", Absent, 5000)}},
		{Date{2026, 9, 14}, NightReported, "Lampu pos mati, perlu diganti.", []AttendanceRow{
			a(1, "Dhani (A-2)", Present, 0), a(2, "Yusuf (B-3)", Absent, 5000), a(3, "C-10", Absent, 5000)}},
		{Date{2026, 9, 8}, NightRainedOut, "", []AttendanceRow{
			a(4, "Gemilang (D-1)", RainedOut, 0), a(5, "E-10", RainedOut, 0)}},
		{Date{2026, 8, 31}, NightReported, "other month", []AttendanceRow{a(1, "Dhani (A-2)", Absent, 5000)}},
	}
}

func TestBuildMonthRecapSummary(t *testing.T) {
	r := BuildMonthRecap(2026, time.September, fixtureNights())
	want := Summary{
		NightsTotal: 4, Reported: 2, Rain: 1, NoReport: 1,
		PresentCount: 3, SlotCount: 8, RatePercent: 38, // 37.5 rounds up
		TotalFine: 25000, FinedMembers: 4,
	}
	if r.Summary != want {
		t.Fatalf("got %+v want %+v", r.Summary, want)
	}
	if r.Year != 2026 || r.Month != time.September {
		t.Errorf("year/month %d %v", r.Year, r.Month)
	}
}

func TestBuildMonthRecapCards(t *testing.T) {
	r := BuildMonthRecap(2026, time.September, fixtureNights())
	if len(r.Cards) != 7 {
		t.Fatalf("want 7 cards, got %d", len(r.Cards))
	}
	mon := r.Cards[0]
	if mon.Weekday != 1 || mon.Label != "Senin" {
		t.Fatalf("card %d %q", mon.Weekday, mon.Label)
	}
	if !reflect.DeepEqual(mon.Dates, []Date{{2026, 9, 7}, {2026, 9, 14}}) {
		t.Errorf("dates %v", mon.Dates)
	}
	wantRows := []CardRow{
		{"C-10", []AttendanceStatus{Absent, Absent}, 10000},
		{"Dhani (A-2)", []AttendanceStatus{Present, Present}, 0},
		{"Yusuf (B-3)", []AttendanceStatus{Present, Absent}, 5000},
	}
	if !reflect.DeepEqual(mon.Rows, wantRows) {
		t.Errorf("rows %+v", mon.Rows)
	}
	if mon.Subtotal != 15000 {
		t.Errorf("subtotal %d", mon.Subtotal)
	}

	wed := r.Cards[2]
	if wed.Label != "Rabu" || len(wed.Dates) != 0 || len(wed.Rows) != 0 || wed.Subtotal != 0 {
		t.Errorf("empty card not empty: %+v", wed)
	}
	tue := r.Cards[1]
	if len(tue.Dates) != 2 || tue.Rows[0].DisplayName != "E-10" || tue.Subtotal != 10000 {
		t.Errorf("tuesday card %+v", tue)
	}
}

func TestBuildMonthRecapTimeline(t *testing.T) {
	r := BuildMonthRecap(2026, time.September, fixtureNights())
	wantDates := []Date{{2026, 9, 15}, {2026, 9, 14}, {2026, 9, 8}, {2026, 9, 7}}
	if len(r.Timeline) != 4 {
		t.Fatalf("len %d", len(r.Timeline))
	}
	for i, d := range wantDates {
		if r.Timeline[i].Date != d {
			t.Errorf("timeline[%d] = %v want %v", i, r.Timeline[i].Date, d)
		}
	}
	tests := []struct {
		idx     int
		weekday string
		note    string
		present int
		total   int
		special bool
	}{
		{0, "Selasa", "Tidak ada laporan sampai pukul 06.00.", 0, 2, false},
		{1, "Senin", "Lampu pos mati, perlu diganti.", 1, 3, true},
		{2, "Selasa", "Hujan, ronda ditiadakan.", 0, 2, false},
		{3, "Senin", "Aman.", 2, 3, false},
	}
	for _, tc := range tests {
		it := r.Timeline[tc.idx]
		if it.Weekday != tc.weekday || it.Note != tc.note || it.Present != tc.present ||
			it.Total != tc.total || it.Special != tc.special {
			t.Errorf("timeline[%d] = %+v", tc.idx, it)
		}
	}
}

func TestRainedOutKeepsCustomNote(t *testing.T) {
	r := BuildMonthRecap(2026, time.September, []NightRow{{Date{2026, 9, 8}, NightRainedOut, " Hujan deras ", nil}})
	if got := r.Timeline[0].Note; got != "Hujan deras" {
		t.Fatalf("got %q", got)
	}
}

func TestRainRequestedCountsAsReported(t *testing.T) {
	r := BuildMonthRecap(2026, time.September, []NightRow{{Date{2026, 9, 8}, NightRainRequested, "", nil}})
	if r.Summary.Reported != 1 || r.Summary.Rain != 0 {
		t.Fatalf("%+v", r.Summary)
	}
}

func TestEmptyMonth(t *testing.T) {
	r := BuildMonthRecap(2026, time.September, nil)
	if r.Summary != (Summary{}) || len(r.Cards) != 7 || len(r.Timeline) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestIsSpecialNote(t *testing.T) {
	tests := map[string]bool{
		"": false, "  ": false, "Aman": false, "aman.": false, " AMAN. ": false,
		"Aman, ada orang asing": true, "Lampu mati": true,
	}
	for in, want := range tests {
		if got := IsSpecialNote(in); got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
}

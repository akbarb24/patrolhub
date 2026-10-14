package domain

import (
	"reflect"
	"testing"
)

func TestCloseNight(t *testing.T) {
	att := func() map[int64]AttendanceStatus {
		return map[int64]AttendanceStatus{1: Present, 2: Absent, 3: Absent}
	}
	tests := []struct {
		name      string
		in        CloseInput
		status    NightStatus
		review    bool
		wantAtt   map[int64]AttendanceStatus
		wantFines map[int64]int
	}{
		{"rained out",
			CloseInput{Status: NightRainedOut, HasReport: true, PhotoCount: 5, MinPhotos: 3, FineRate: 5000, Attendance: att()},
			NightRainedOut, false,
			map[int64]AttendanceStatus{1: RainedOut, 2: RainedOut, 3: RainedOut},
			map[int64]int{1: 0, 2: 0, 3: 0}},
		{"rain requested waits for admin",
			CloseInput{Status: NightRainRequested, HasReport: true, PhotoCount: 5, MinPhotos: 3, FineRate: 5000, Attendance: att()},
			NightRainRequested, true, att(),
			map[int64]int{1: 0, 2: 0, 3: 0}},
		{"no report fines everyone",
			CloseInput{Status: NightOpen, HasReport: false, MinPhotos: 3, FineRate: 5000, Attendance: att()},
			NightNoReport, false,
			map[int64]AttendanceStatus{1: Absent, 2: Absent, 3: Absent},
			map[int64]int{1: 5000, 2: 5000, 3: 5000}},
		{"too few photos needs review",
			CloseInput{Status: NightReported, HasReport: true, PhotoCount: 2, MinPhotos: 3, FineRate: 5000, Attendance: att()},
			NightReported, true, att(),
			map[int64]int{1: 0, 2: 5000, 3: 5000}},
		{"complete report",
			CloseInput{Status: NightReported, HasReport: true, PhotoCount: 3, MinPhotos: 3, FineRate: 5000, Attendance: att()},
			NightReported, false, att(),
			map[int64]int{1: 0, 2: 5000, 3: 5000}},
		{"everyone present, 3 photos, no fines",
			CloseInput{Status: NightReported, HasReport: true, PhotoCount: 3, MinPhotos: 3, FineRate: 5000,
				Attendance: map[int64]AttendanceStatus{1: Present, 2: Present, 3: Present, 4: Present}},
			NightReported, false,
			map[int64]AttendanceStatus{1: Present, 2: Present, 3: Present, 4: Present},
			map[int64]int{1: 0, 2: 0, 3: 0, 4: 0}},
		{"nil attendance",
			CloseInput{Status: NightReported, HasReport: true, PhotoCount: 3, MinPhotos: 3, FineRate: 5000},
			NightReported, false, map[int64]AttendanceStatus{}, map[int64]int{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := map[int64]AttendanceStatus{}
			for k, v := range tc.in.Attendance {
				before[k] = v
			}
			got := CloseNight(tc.in)
			if got.Status != tc.status || got.NeedsReview != tc.review {
				t.Errorf("status=%s review=%v", got.Status, got.NeedsReview)
			}
			if !reflect.DeepEqual(got.Attendance, tc.wantAtt) {
				t.Errorf("attendance %v want %v", got.Attendance, tc.wantAtt)
			}
			if !reflect.DeepEqual(got.Fines, tc.wantFines) {
				t.Errorf("fines %v want %v", got.Fines, tc.wantFines)
			}
			if len(tc.in.Attendance) > 0 && !reflect.DeepEqual(tc.in.Attendance, before) {
				t.Error("input attendance was mutated")
			}
		})
	}
}

func TestRecalculateFines(t *testing.T) {
	got := RecalculateFines(map[int64]AttendanceStatus{1: Present, 2: Absent, 3: RainedOut}, 7000)
	want := map[int64]int{1: 0, 2: 7000, 3: 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

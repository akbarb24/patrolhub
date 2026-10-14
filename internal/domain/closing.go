package domain

import "maps"

type CloseInput struct {
	Status     NightStatus
	HasReport  bool // first_reported_at != nil
	PhotoCount int
	MinPhotos  int
	FineRate   int
	Attendance map[int64]AttendanceStatus // member_id -> current status
}

type CloseResult struct {
	Status      NightStatus
	NeedsReview bool
	Attendance  map[int64]AttendanceStatus
	Fines       map[int64]int
}

// CloseNight applies the closing rules (first matching row wins):
//
//	RAINED_OUT      -> all RAINED_OUT, no fines
//	RAIN_REQUESTED  -> unchanged, no fines, needs admin review
//	no report       -> NO_REPORT, everyone ABSENT and fined
//	too few photos  -> REPORTED, fines for ABSENT, needs review
//	otherwise       -> REPORTED, fines for ABSENT
func CloseNight(in CloseInput) CloseResult {
	att := maps.Clone(in.Attendance)
	if att == nil {
		att = map[int64]AttendanceStatus{}
	}

	switch {
	case in.Status == NightRainedOut:
		for id := range att {
			att[id] = RainedOut
		}
		return CloseResult{NightRainedOut, false, att, RecalculateFines(att, in.FineRate)}

	case in.Status == NightRainRequested:
		return CloseResult{NightRainRequested, true, att, RecalculateFines(att, 0)}

	case !in.HasReport:
		for id := range att {
			att[id] = Absent
		}
		return CloseResult{NightNoReport, false, att, RecalculateFines(att, in.FineRate)}
	}

	return CloseResult{
		Status:      NightReported,
		NeedsReview: in.PhotoCount < in.MinPhotos,
		Attendance:  att,
		Fines:       RecalculateFines(att, in.FineRate),
	}
}

// RecalculateFines gives fineRate to every ABSENT member and 0 to everyone else.
func RecalculateFines(statuses map[int64]AttendanceStatus, fineRate int) map[int64]int {
	fines := make(map[int64]int, len(statuses))
	for id, st := range statuses {
		if st == Absent {
			fines[id] = fineRate
		} else {
			fines[id] = 0
		}
	}
	return fines
}

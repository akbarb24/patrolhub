package domain

import (
	"sort"
	"strings"
	"time"
)

type NightRow struct {
	Date       Date
	Status     NightStatus
	Note       string
	Attendance []AttendanceRow
}

type AttendanceRow struct {
	MemberID    int64
	DisplayName string
	Status      AttendanceStatus
	Fine        int
}

type MonthRecap struct {
	Year     int
	Month    time.Month
	Summary  Summary
	Cards    []DayCard      // 7 cards, Monday..Sunday; a card without nights is present but empty
	Timeline []TimelineItem // newest first
}

type Summary struct {
	NightsTotal, Reported, Rain, NoReport int
	PresentCount, SlotCount               int // SlotCount = PRESENT + ABSENT (rain is not counted)
	RatePercent                           int // rounded; 0 when SlotCount is 0
	TotalFine                             int
	FinedMembers                          int
}

type DayCard struct {
	Weekday  int
	Label    string // "Senin"
	Dates    []Date // closed nights with this weekday, ascending
	Rows     []CardRow
	Subtotal int
}

type CardRow struct {
	DisplayName string
	Cells       []AttendanceStatus // one per Dates entry; "" when the member was not on duty
	Fine        int
}

type TimelineItem struct {
	Date           Date
	Weekday        string
	Status         NightStatus
	Present, Total int
	Note           string
	Special        bool // the situation note is more than a plain "aman"
}

// BuildMonthRecap builds the public recap. nights must contain only closed nights;
// nights outside the given month are ignored. RAIN_REQUESTED counts as reported
// until an admin confirms the rain.
func BuildMonthRecap(year int, month time.Month, nights []NightRow) MonthRecap {
	var in []NightRow
	for _, n := range nights {
		if n.Date.Year == year && n.Date.Month == month {
			in = append(in, n)
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Date.Compare(in[j].Date) < 0 })

	return MonthRecap{
		Year:     year,
		Month:    month,
		Summary:  buildSummary(in),
		Cards:    buildCards(in),
		Timeline: buildTimeline(in),
	}
}

func buildSummary(nights []NightRow) Summary {
	s := Summary{NightsTotal: len(nights)}
	fineByMember := map[int64]int{}
	for _, n := range nights {
		switch n.Status {
		case NightReported, NightRainRequested:
			s.Reported++
		case NightRainedOut:
			s.Rain++
		case NightNoReport:
			s.NoReport++
		}
		for _, a := range n.Attendance {
			switch a.Status {
			case Present:
				s.PresentCount++
				s.SlotCount++
			case Absent:
				s.SlotCount++
			}
			s.TotalFine += a.Fine
			fineByMember[a.MemberID] += a.Fine
		}
	}
	if s.SlotCount > 0 {
		s.RatePercent = (s.PresentCount*200 + s.SlotCount) / (2 * s.SlotCount) // round half up
	}
	for _, f := range fineByMember {
		if f > 0 {
			s.FinedMembers++
		}
	}
	return s
}

func buildCards(nights []NightRow) []DayCard {
	cards := make([]DayCard, 7)
	for wd := 1; wd <= 7; wd++ {
		var group []NightRow
		for _, n := range nights {
			if n.Date.ISOWeekday() == wd {
				group = append(group, n)
			}
		}

		card := DayCard{Weekday: wd, Label: WeekdayNameID(wd)}
		rows := map[int64]*CardRow{}
		for i, n := range group {
			card.Dates = append(card.Dates, n.Date)
			for _, a := range n.Attendance {
				row, ok := rows[a.MemberID]
				if !ok {
					row = &CardRow{DisplayName: a.DisplayName, Cells: make([]AttendanceStatus, len(group))}
					rows[a.MemberID] = row
				}
				row.Cells[i] = a.Status
				row.Fine += a.Fine
			}
		}
		for _, row := range rows {
			card.Rows = append(card.Rows, *row)
			card.Subtotal += row.Fine
		}
		sort.Slice(card.Rows, func(i, j int) bool {
			a, b := strings.ToLower(card.Rows[i].DisplayName), strings.ToLower(card.Rows[j].DisplayName)
			if a != b {
				return a < b
			}
			return card.Rows[i].DisplayName < card.Rows[j].DisplayName
		})
		cards[wd-1] = card
	}
	return cards
}

func buildTimeline(nights []NightRow) []TimelineItem {
	items := make([]TimelineItem, 0, len(nights))
	for i := len(nights) - 1; i >= 0; i-- { // nights are ascending; timeline is newest first
		n := nights[i]
		item := TimelineItem{
			Date:    n.Date,
			Weekday: WeekdayNameID(n.Date.ISOWeekday()),
			Status:  n.Status,
			Total:   len(n.Attendance),
			Note:    strings.TrimSpace(n.Note),
			Special: IsSpecialNote(n.Note),
		}
		for _, a := range n.Attendance {
			if a.Status == Present {
				item.Present++
			}
		}
		switch {
		case n.Status == NightNoReport:
			item.Note = "Tidak ada laporan sampai pukul 06.00."
		case n.Status == NightRainedOut && item.Note == "":
			item.Note = "Hujan, ronda ditiadakan."
		}
		items = append(items, item)
	}
	return items
}

// IsSpecialNote reports whether a situation note says more than a plain "aman".
func IsSpecialNote(note string) bool {
	n := strings.ToLower(strings.TrimSpace(note))
	n = strings.TrimSpace(strings.TrimRight(n, "."))
	return n != "" && n != "aman"
}

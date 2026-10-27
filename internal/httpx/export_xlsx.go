package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/service"
)

// Excel export: the grid the RT kept by hand before PatrolHub (members grouped by
// weekday, one column per day of the month, v = present, 1 = absent, h = rain).

// weekdayLetters are the column headers of the old sheet, by ISO weekday (Senin = 1).
var weekdayLetters = [8]string{"", "S", "Sl", "R", "K", "J", "S", "M"}

func (h adminHandlers) exportXLSX(w http.ResponseWriter, r *http.Request) {
	month, ok := finePeriod(r)
	if !ok {
		notFoundPage(w)
		return
	}
	rec, err := h.Recap.Month(r.Context(), month.Year(), month.Month(), h.today())
	switch {
	case errors.Is(err, service.ErrFutureMonth), errors.Is(err, service.ErrNotFound):
		notFoundPage(w)
		return
	case err != nil:
		serverError(w, "export xlsx", err)
		return
	}
	f, err := recapWorkbook(rec.MonthRecap)
	if err != nil {
		serverError(w, "export xlsx", err)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="rekap-ronda-%04d-%02d.xlsx"`, month.Year(), int(month.Month())))
	if err := f.Write(w); err != nil {
		serverError(w, "export xlsx", err)
	}
}

// recapWorkbook lays out a month recap as a single sheet.
func recapWorkbook(rec domain.MonthRecap) (*excelize.File, error) {
	f := excelize.NewFile()
	sheet := fmt.Sprintf("%s %d", domain.MonthNameID(rec.Month), rec.Year)
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}

	border := []excelize.Border{
		{Type: "left", Color: "000000", Style: 1}, {Type: "right", Color: "000000", Style: 1},
		{Type: "top", Color: "000000", Style: 1}, {Type: "bottom", Color: "000000", Style: 1},
	}
	center := &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}
	rp := `"Rp "#,##0`
	newStyle := func(s excelize.Style) int {
		s.Border, s.Alignment = border, center
		id, _ := f.NewStyle(&s) // only fails on invalid style definitions, which are fixed here
		return id
	}
	header := newStyle(excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"B4C6E7"}}})
	headerRp := newStyle(excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"B4C6E7"}}, CustomNumFmt: &rp})
	offDuty := newStyle(excelize.Style{Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"808080"}}})
	plain := newStyle(excelize.Style{})
	money := newStyle(excelize.Style{CustomNumFmt: &rp})

	days := time.Date(rec.Year, rec.Month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	fineCol := days + 3 // A = weekday, B = member, C.. = days
	cell := func(col, row int) string {
		name, _ := excelize.CoordinatesToCellName(col, row) // col and row are always ≥ 1
		return name
	}
	set := func(col, row int, v any, style int) {
		f.SetCellValue(sheet, cell(col, row), v)
		f.SetCellStyle(sheet, cell(col, row), cell(col, row), style)
	}

	monthTitle := domain.MonthNameID(rec.Month)
	set(1, 1, "Bulan "+monthTitle, header)
	set(1, 2, nil, header)
	f.MergeCell(sheet, "A1", "A2")
	set(2, 1, "Hari", header)
	set(2, 2, "Tanggal", header)
	for d := 1; d <= days; d++ {
		set(d+2, 1, weekdayLetters[domain.Date{Year: rec.Year, Month: rec.Month, Day: d}.ISOWeekday()], header)
		set(d+2, 2, d, header)
	}
	set(fineCol, 1, "Denda Ronda Bulan "+monthTitle, header)
	set(fineCol, 2, nil, header)
	f.MergeCell(sheet, cell(fineCol, 1), cell(fineCol, 2))

	row := 3
	for _, card := range rec.Cards {
		if len(card.Rows) == 0 {
			continue
		}
		first := row
		for _, r := range card.Rows {
			set(1, row, card.Label, header)
			set(2, row, r.DisplayName, header)
			f.SetCellStyle(sheet, cell(3, row), cell(days+2, row), offDuty)
			for i, status := range r.Cells {
				col := card.Dates[i].Day + 2
				switch status {
				case domain.Present:
					set(col, row, "v", plain)
				case domain.Absent:
					set(col, row, 1, plain)
				case domain.RainedOut:
					set(col, row, "h", plain)
				}
			}
			set(fineCol, row, r.Fine, money)
			row++
		}
		f.MergeCell(sheet, cell(1, first), cell(1, row-1))
	}

	set(1, row, "TOTAL DENDA RONDA", header)
	f.SetCellStyle(sheet, cell(1, row), cell(fineCol-1, row), header)
	f.MergeCell(sheet, cell(1, row), cell(fineCol-1, row))
	set(fineCol, row, rec.Summary.TotalFine, headerRp)

	col := func(n int) string {
		name, _ := excelize.ColumnNumberToName(n)
		return name
	}
	f.SetColWidth(sheet, "A", "A", 12)
	f.SetColWidth(sheet, "B", "B", 20)
	f.SetColWidth(sheet, "C", col(days+2), 4)
	f.SetColWidth(sheet, col(fineCol), col(fineCol), 18)
	return f, nil
}

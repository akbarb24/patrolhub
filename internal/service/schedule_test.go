package service

import (
	"context"
	"errors"
	"testing"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/testdb"
)

func mustMember(t *testing.T, ms *MemberService, block, name string) db.Member {
	t.Helper()
	m, err := ms.Create(context.Background(), MemberInput{HouseBlock: block, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func teamNames(entries []TeamEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.DisplayName
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMemberNormalizationAndDisplay(t *testing.T) {
	ms := NewMemberService(testdb.New(t))
	ctx := context.Background()

	m, err := ms.Create(ctx, MemberInput{HouseBlock: " c-10 "})
	if err != nil {
		t.Fatal(err)
	}
	if m.HouseBlock != "C-10" || m.Name != nil || MemberLabel(m) != "C-10" {
		t.Fatalf("block-only member: %+v label=%q", m, MemberLabel(m))
	}
	named := mustMember(t, ms, "a-2", "Dhani")
	if MemberLabel(named) != "Dhani (A-2)" {
		t.Fatalf("label %q", MemberLabel(named))
	}

	if _, err := ms.Create(ctx, MemberInput{HouseBlock: "  "}); !errors.As(err, new(ValidationErrors)) {
		t.Fatalf("blank block must fail validation: %v", err)
	}

	upd, err := ms.Update(ctx, named.ID, MemberInput{HouseBlock: "a-3", Name: "", Phone: " 0812 ", Notes: "x"})
	if err != nil || upd.HouseBlock != "A-3" || upd.Name != nil || *upd.Phone != "0812" {
		t.Fatalf("update: %+v %v", upd, err)
	}
	if _, err := ms.Update(ctx, 9999, MemberInput{HouseBlock: "A-1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}

	if err := ms.SetActive(ctx, m.ID, false); err != nil {
		t.Fatal(err)
	}
	active, _ := ms.ListActive(ctx)
	all, _ := ms.List(ctx)
	if len(active) != 1 || len(all) != 2 {
		t.Fatalf("active=%d all=%d", len(active), len(all))
	}
}

func TestScheduleRulesAndOverrides(t *testing.T) {
	pool := testdb.New(t)
	ms, ss := NewMemberService(pool), NewScheduleService(pool)
	ctx := context.Background()
	anton, budi, citra := mustMember(t, ms, "A-1", "Anton"), mustMember(t, ms, "B-2", "Budi"), mustMember(t, ms, "C-3", "")

	monday := domain.Date{Year: 2026, Month: 10, Day: 5}
	nextMonday := monday.AddDays(7)
	from := domain.Date{Year: 2026, Month: 1, Day: 1}
	for _, m := range []db.Member{anton, budi} {
		if err := ss.AddRule(ctx, m.ID, 1, from); err != nil {
			t.Fatal(err)
		}
	}
	if err := ss.AddRule(ctx, anton.ID, 1, from); !errors.As(err, new(ValidationErrors)) {
		t.Fatalf("duplicate weekday must fail: %v", err)
	}
	if err := ss.AddRule(ctx, anton.ID, 8, from); !errors.As(err, new(ValidationErrors)) {
		t.Fatalf("bad weekday must fail: %v", err)
	}

	team, err := ss.Team(ctx, monday)
	if err != nil || !equal(teamNames(team), []string{"Anton (A-1)", "Budi (B-2)"}) {
		t.Fatalf("team %v %v", teamNames(team), err)
	}

	// Swap Budi for the block-only member on next Monday only.
	if err := ss.CreateOverride(ctx, OverrideInput{Date: nextMonday, Action: "SWAP", MemberID: budi.ID, ReplacementID: &citra.ID, Note: "sakit"}); err != nil {
		t.Fatal(err)
	}
	team, _ = ss.Team(ctx, nextMonday)
	if !equal(teamNames(team), []string{"Anton (A-1)", "C-3"}) {
		t.Fatalf("swapped team %v", teamNames(team))
	}
	team, _ = ss.Team(ctx, monday)
	if len(team) != 2 || team[1].DisplayName != "Budi (B-2)" {
		t.Fatalf("swap leaked into other night: %v", teamNames(team))
	}

	if err := ss.CreateOverride(ctx, OverrideInput{Date: nextMonday, Action: "SWAP", MemberID: budi.ID}); !errors.As(err, new(ValidationErrors)) {
		t.Fatalf("SWAP without replacement: %v", err)
	}
	if err := ss.CreateOverride(ctx, OverrideInput{Date: nextMonday, Action: "SWAP", MemberID: budi.ID, ReplacementID: &budi.ID}); !errors.As(err, new(ValidationErrors)) {
		t.Fatalf("SWAP with self: %v", err)
	}

	list, err := ss.Overrides(ctx, monday)
	if err != nil || len(list) != 1 || !list[0].Deletable || list[0].Replacement != "C-3" || list[0].Note != "sakit" {
		t.Fatalf("overrides %+v %v", list, err)
	}

	// Closing the night freezes its overrides.
	q := db.New(pool)
	if err := q.InsertNightIfMissing(ctx, pgDate(nextMonday)); err != nil {
		t.Fatal(err)
	}
	n, _ := q.GetNightByDate(ctx, pgDate(nextMonday))
	if err := q.CloseNight(ctx, db.CloseNightParams{ID: n.ID, Status: "REPORTED", ClosedAt: pgTime(testNow)}); err != nil {
		t.Fatal(err)
	}
	if err := ss.DeleteOverride(ctx, list[0].ID); !errors.Is(err, ErrNightClosed) {
		t.Fatalf("delete on closed night: %v", err)
	}
	if err := ss.CreateOverride(ctx, OverrideInput{Date: nextMonday, Action: "ADD", MemberID: citra.ID}); !errors.As(err, new(ValidationErrors)) {
		t.Fatalf("override on closed night: %v", err)
	}
	if err := ss.DeleteOverride(ctx, 424242); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown override: %v", err)
	}
}

func TestEndRuleKeepsHistory(t *testing.T) {
	pool := testdb.New(t)
	ms, ss := NewMemberService(pool), NewScheduleService(pool)
	ctx := context.Background()
	anton := mustMember(t, ms, "A-1", "Anton")

	today := domain.Date{Year: 2026, Month: 10, Day: 7}
	if err := ss.AddRule(ctx, anton.ID, 1, domain.Date{Year: 2026, Month: 1, Day: 1}); err != nil {
		t.Fatal(err)
	}
	if err := ss.AddRule(ctx, anton.ID, 3, today.AddDays(5)); err != nil { // not started yet
		t.Fatal(err)
	}
	week, err := ss.Week(ctx, today)
	if err != nil || len(week[0]) != 1 || len(week[2]) != 1 || !week[2][0].Upcoming || week[0][0].Upcoming {
		t.Fatalf("week %+v %v", week, err)
	}

	if err := ss.EndRule(ctx, week[0][0].ID, today); err != nil {
		t.Fatal(err)
	}
	if err := ss.EndRule(ctx, week[2][0].ID, today); err != nil {
		t.Fatal(err)
	}

	// The old Monday still has Anton; Mondays from today on do not.
	if team, _ := ss.Team(ctx, domain.Date{Year: 2026, Month: 10, Day: 5}); len(team) != 1 {
		t.Fatalf("history changed: %v", teamNames(team))
	}
	if team, _ := ss.Team(ctx, domain.Date{Year: 2026, Month: 10, Day: 12}); len(team) != 0 {
		t.Fatalf("future Monday still has %v", teamNames(team))
	}
	week, _ = ss.Week(ctx, today)
	if len(week[0]) != 0 || len(week[2]) != 0 {
		t.Fatalf("ended rules still listed: %+v", week)
	}
	if err := ss.EndRule(ctx, 9999, today); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown rule: %v", err)
	}
}

func TestInactiveMemberLeavesNewTeams(t *testing.T) {
	pool := testdb.New(t)
	ms, ss := NewMemberService(pool), NewScheduleService(pool)
	ctx := context.Background()
	anton := mustMember(t, ms, "A-1", "Anton")
	_ = ss.AddRule(ctx, anton.ID, 1, domain.Date{Year: 2026, Month: 1, Day: 1})
	_ = ms.SetActive(ctx, anton.ID, false)
	if team, _ := ss.Team(ctx, domain.Date{Year: 2026, Month: 10, Day: 5}); len(team) != 0 {
		t.Fatalf("inactive member in team: %v", teamNames(team))
	}
	if err := ss.AddRule(ctx, anton.ID, 2, domain.Date{Year: 2026, Month: 1, Day: 1}); !errors.As(err, new(ValidationErrors)) {
		t.Fatalf("scheduling an inactive member: %v", err)
	}
}

func TestMonthPreview(t *testing.T) {
	pool := testdb.New(t)
	ms, ss := NewMemberService(pool), NewScheduleService(pool)
	ctx := context.Background()
	anton := mustMember(t, ms, "A-1", "Anton")
	_ = ss.AddRule(ctx, anton.ID, 1, domain.Date{Year: 2026, Month: 1, Day: 1})
	days, err := ss.MonthPreview(ctx, 2026, 10)
	if err != nil || len(days) != 31 {
		t.Fatalf("days=%d err=%v", len(days), err)
	}
	mondays := 0
	for _, d := range days {
		if len(d.Team) > 0 {
			mondays++
			if d.Date.ISOWeekday() != 1 {
				t.Errorf("%v has a team but is not a Monday", d.Date)
			}
		}
	}
	if mondays != 4 { // 5, 12, 19, 26 October 2026
		t.Fatalf("mondays with team: %d", mondays)
	}
}

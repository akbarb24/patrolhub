package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
)

var ErrDuplicateRule = errors.New("member already scheduled on that weekday")

// RuleView is one fixed weekly duty for display.
type RuleView struct {
	ID          int64
	MemberID    int64
	DisplayName string
	DayOfWeek   int
	From        domain.Date
	To          *domain.Date
	Upcoming    bool // starts after today
}

// OverrideView is a per-date change for display.
type OverrideView struct {
	ID          int64
	Date        domain.Date
	Action      string // ADD, REMOVE, SWAP
	Member      string
	Replacement string
	Note        string
	Deletable   bool // only while that night is not closed yet
}

type TeamEntry struct {
	MemberID    int64
	DisplayName string
	Source      domain.AttendanceSource
}

type DayTeam struct {
	Date domain.Date
	Team []TeamEntry
}

type OverrideInput struct {
	Date          domain.Date
	Action        string
	MemberID      int64
	ReplacementID *int64
	Note          string
}

type ScheduleService struct {
	pool *pgxpool.Pool
}

func NewScheduleService(pool *pgxpool.Pool) *ScheduleService { return &ScheduleService{pool: pool} }

// Week lists the rules per ISO weekday (index 0 = Monday) that are running today or start later.
func (s *ScheduleService) Week(ctx context.Context, today domain.Date) ([7][]RuleView, error) {
	var week [7][]RuleView
	q := db.New(s.pool)
	rules, err := q.ListCurrentRules(ctx, pgDate(today))
	if err != nil {
		return week, err
	}
	labels, err := memberLabels(ctx, q)
	if err != nil {
		return week, err
	}
	for _, r := range rules {
		if r.DayOfWeek < 1 || r.DayOfWeek > 7 {
			continue
		}
		v := RuleView{
			ID: r.ID, MemberID: r.MemberID, DisplayName: labels[r.MemberID], DayOfWeek: int(r.DayOfWeek),
			From: domainDate(r.EffectiveFrom), To: domainDatePtr(r.EffectiveTo),
		}
		v.Upcoming = v.From.Compare(today) > 0
		week[v.DayOfWeek-1] = append(week[v.DayOfWeek-1], v)
	}
	for i := range week {
		sort.Slice(week[i], func(a, b int) bool { return week[i][a].DisplayName < week[i][b].DisplayName })
	}
	return week, nil
}

// AddRule puts a member on a weekday starting at from.
func (s *ScheduleService) AddRule(ctx context.Context, memberID int64, dayOfWeek int, from domain.Date) error {
	errs := ValidationErrors{}
	if dayOfWeek < 1 || dayOfWeek > 7 {
		errs["day_of_week"] = "Pilih hari jaga."
	}
	if len(errs) > 0 {
		return errs
	}
	return WithTx(ctx, s.pool, func(q *db.Queries) error {
		m, err := q.GetMember(ctx, memberID)
		if err != nil || !m.Active {
			return ValidationErrors{"member_id": "Pilih peserta aktif."}
		}
		existing, err := q.ListRulesByMember(ctx, memberID)
		if err != nil {
			return err
		}
		for _, r := range existing {
			// An open-ended or still-running rule for the same weekday would double the duty.
			if int(r.DayOfWeek) == dayOfWeek && (!r.EffectiveTo.Valid || domainDate(r.EffectiveTo).Compare(from) >= 0) {
				return ValidationErrors{"member_id": "Peserta sudah terjadwal di hari itu."}
			}
		}
		_, err = q.CreateRule(ctx, db.CreateRuleParams{MemberID: memberID, DayOfWeek: int16(dayOfWeek), EffectiveFrom: pgDate(from)})
		return err
	})
}

// EndRule takes a member off the schedule from today on: effective_to becomes
// yesterday, so past nights keep their team. A rule that has not started yet has
// no history and is simply deleted.
func (s *ScheduleService) EndRule(ctx context.Context, id int64, today domain.Date) error {
	return WithTx(ctx, s.pool, func(q *db.Queries) error {
		r, err := q.GetRule(ctx, id)
		if err != nil {
			return notFound(err)
		}
		yesterday := today.AddDays(-1)
		if domainDate(r.EffectiveFrom).Compare(yesterday) > 0 {
			return q.DeleteRule(ctx, id)
		}
		return q.EndRule(ctx, db.EndRuleParams{ID: id, EffectiveTo: pgDate(yesterday)})
	})
}

// Overrides lists changes from 7 days back to 30 days ahead of today.
func (s *ScheduleService) Overrides(ctx context.Context, today domain.Date) ([]OverrideView, error) {
	q := db.New(s.pool)
	from, to := today.AddDays(-7), today.AddDays(30)
	list, err := q.ListOverridesBetween(ctx, db.ListOverridesBetweenParams{FromDate: pgDate(from), ToDate: pgDate(to)})
	if err != nil {
		return nil, err
	}
	nights, err := q.ListNightsBetween(ctx, db.ListNightsBetweenParams{FromDate: pgDate(from), ToDate: pgDate(to)})
	if err != nil {
		return nil, err
	}
	closed := map[domain.Date]bool{}
	for _, n := range nights {
		if n.ClosedAt.Valid {
			closed[domainDate(n.PatrolDate)] = true
		}
	}
	labels, err := memberLabels(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]OverrideView, len(list))
	for i, o := range list {
		d := domainDate(o.PatrolDate)
		v := OverrideView{ID: o.ID, Date: d, Action: o.Action, Member: labels[o.MemberID], Note: deref(o.Note), Deletable: !closed[d]}
		if o.ReplacementMemberID != nil {
			v.Replacement = labels[*o.ReplacementMemberID]
		}
		out[i] = v
	}
	return out, nil
}

// CreateOverride records an ADD, REMOVE or SWAP for one night that is not closed yet.
func (s *ScheduleService) CreateOverride(ctx context.Context, in OverrideInput) error {
	errs := ValidationErrors{}
	switch in.Action {
	case "ADD", "REMOVE":
		in.ReplacementID = nil
	case "SWAP":
		if in.ReplacementID == nil {
			errs["replacement_member_id"] = "Pilih peserta pengganti."
		} else if *in.ReplacementID == in.MemberID {
			errs["replacement_member_id"] = "Pengganti harus peserta yang berbeda."
		}
	default:
		errs["action"] = "Pilih aksi."
	}
	if in.MemberID == 0 {
		errs["member_id"] = "Pilih peserta."
	}
	if len(errs) > 0 {
		return errs
	}

	return WithTx(ctx, s.pool, func(q *db.Queries) error {
		if n, err := q.GetNightByDate(ctx, pgDate(in.Date)); err == nil && n.ClosedAt.Valid {
			return ValidationErrors{"patrol_date": "Malam itu sudah ditutup."}
		}
		ids := []int64{in.MemberID}
		if in.ReplacementID != nil {
			ids = append(ids, *in.ReplacementID)
		}
		for _, id := range ids {
			if _, err := q.GetMember(ctx, id); err != nil {
				return ValidationErrors{"member_id": "Peserta tidak ditemukan."}
			}
		}
		_, err := q.CreateOverride(ctx, db.CreateOverrideParams{
			PatrolDate: pgDate(in.Date), Action: in.Action, MemberID: in.MemberID,
			ReplacementMemberID: in.ReplacementID, Note: optional(in.Note),
		})
		return err
	})
}

// DeleteOverride removes a change, but only while its night is not closed.
func (s *ScheduleService) DeleteOverride(ctx context.Context, id int64) error {
	return WithTx(ctx, s.pool, func(q *db.Queries) error {
		o, err := q.GetOverride(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if n, err := q.GetNightByDate(ctx, o.PatrolDate); err == nil && n.ClosedAt.Valid {
			return ErrNightClosed
		}
		return q.DeleteOverride(ctx, id)
	})
}

// Team previews the team of a night (fixed schedule plus overrides).
func (s *ScheduleService) Team(ctx context.Context, date domain.Date) ([]TeamEntry, error) {
	return teamEntries(ctx, db.New(s.pool), date)
}

// MonthPreview lists the team of every day of a month (F1.5).
func (s *ScheduleService) MonthPreview(ctx context.Context, year int, month time.Month) ([]DayTeam, error) {
	q := db.New(s.pool)
	var out []DayTeam
	for d := (domain.Date{Year: year, Month: month, Day: 1}); d.Month == month; d = d.AddDays(1) {
		team, err := teamEntries(ctx, q, d)
		if err != nil {
			return nil, err
		}
		out = append(out, DayTeam{Date: d, Team: team})
	}
	return out, nil
}

func teamEntries(ctx context.Context, q *db.Queries, date domain.Date) ([]TeamEntry, error) {
	team, err := resolveTeam(ctx, q, date)
	if err != nil {
		return nil, err
	}
	labels, err := memberLabels(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]TeamEntry, len(team))
	for i, m := range team {
		out[i] = TeamEntry{MemberID: m.MemberID, DisplayName: labels[m.MemberID], Source: m.Source}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DisplayName < out[j].DisplayName })
	return out, nil
}

func memberLabels(ctx context.Context, q *db.Queries) (map[int64]string, error) {
	members, err := q.ListMembers(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]string, len(members))
	for _, x := range members {
		m[x.ID] = MemberLabel(x)
	}
	return m, nil
}

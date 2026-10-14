package service

import (
	"context"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
)

// resolveTeam loads the schedule data for a night and runs domain.ResolveTeam.
func resolveTeam(ctx context.Context, q *db.Queries, night domain.Date) ([]domain.TeamMember, error) {
	rules, err := q.ListRulesForDate(ctx, db.ListRulesForDateParams{Dow: int16(night.ISOWeekday()), OnDate: pgDate(night)})
	if err != nil {
		return nil, err
	}
	overrides, err := q.ListOverridesForDate(ctx, pgDate(night))
	if err != nil {
		return nil, err
	}
	members, err := q.ListActiveMembers(ctx)
	if err != nil {
		return nil, err
	}

	active := make(map[int64]bool, len(members))
	for _, m := range members {
		active[m.ID] = true
	}
	dr := make([]domain.Rule, len(rules))
	for i, r := range rules {
		dr[i] = domain.Rule{MemberID: r.MemberID, DayOfWeek: int(r.DayOfWeek), From: domainDate(r.EffectiveFrom), To: domainDatePtr(r.EffectiveTo)}
	}
	do := make([]domain.Override, len(overrides))
	for i, o := range overrides {
		do[i] = domain.Override{Action: o.Action, MemberID: o.MemberID, ReplacementID: o.ReplacementMemberID}
	}
	return domain.ResolveTeam(night, dr, do, active), nil
}

// syncTeam makes the night's attendance rows match the resolved team: it adds
// missing members as ABSENT and removes ABSENT rows of people no longer on the team.
// PRESENT and WALK_IN rows are never removed.
func syncTeam(ctx context.Context, q *db.Queries, night db.PatrolNight) error {
	team, err := resolveTeam(ctx, q, domainDate(night.PatrolDate))
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(team))
	for _, m := range team {
		ids = append(ids, m.MemberID)
		err := q.UpsertAttendance(ctx, db.UpsertAttendanceParams{
			NightID: night.ID, MemberID: m.MemberID, Status: string(domain.Absent), Source: string(m.Source),
		})
		if err != nil {
			return err
		}
	}
	return q.DeleteAbsentAttendanceNotIn(ctx, db.DeleteAbsentAttendanceNotInParams{NightID: night.ID, MemberIds: ids})
}

package domain

import "sort"

type Rule struct {
	MemberID  int64
	DayOfWeek int
	From      Date
	To        *Date
}

type Override struct {
	Action        string // "ADD", "REMOVE" or "SWAP"
	MemberID      int64
	ReplacementID *int64
}

type TeamMember struct {
	MemberID int64
	Source   AttendanceSource
}

// ResolveTeam computes the team of a night: the fixed weekly rules that apply on
// that date (for active members), then the per-date overrides in the given order.
// The result has no duplicates and is sorted by member id.
func ResolveTeam(night Date, rules []Rule, overrides []Override, activeIDs map[int64]bool) []TeamMember {
	team := map[int64]AttendanceSource{}
	dow := night.ISOWeekday()

	for _, r := range rules {
		if r.DayOfWeek != dow || r.From.Compare(night) > 0 {
			continue
		}
		if r.To != nil && night.Compare(*r.To) > 0 {
			continue
		}
		if !activeIDs[r.MemberID] {
			continue
		}
		team[r.MemberID] = SourceScheduled
	}

	add := func(id int64) {
		if _, ok := team[id]; !ok {
			team[id] = SourceOverride
		}
	}
	for _, o := range overrides {
		switch o.Action {
		case "ADD":
			add(o.MemberID)
		case "REMOVE":
			delete(team, o.MemberID)
		case "SWAP":
			delete(team, o.MemberID)
			if o.ReplacementID != nil {
				add(*o.ReplacementID)
			}
		}
	}

	out := make([]TeamMember, 0, len(team))
	for id, src := range team {
		out = append(out, TeamMember{id, src})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MemberID < out[j].MemberID })
	return out
}

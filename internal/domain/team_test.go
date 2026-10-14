package domain

import (
	"reflect"
	"testing"
)

func TestResolveTeam(t *testing.T) {
	monday := Date{2026, 10, 5}
	ptr := func(d Date) *Date { return &d }
	id := func(v int64) *int64 { return &v }
	active := map[int64]bool{1: true, 2: true, 3: true, 4: true, 9: true}
	mon := func(m int64) Rule { return Rule{MemberID: m, DayOfWeek: 1, From: Date{2026, 1, 1}} }
	sched := func(ids ...int64) []TeamMember {
		var out []TeamMember
		for _, i := range ids {
			out = append(out, TeamMember{i, SourceScheduled})
		}
		return out
	}

	tests := []struct {
		name      string
		night     Date
		rules     []Rule
		overrides []Override
		active    map[int64]bool
		want      []TeamMember
	}{
		{"three Monday rules", monday, []Rule{mon(3), mon(1), mon(2)}, nil, active, sched(1, 2, 3)},
		{"wrong weekday", Date{2026, 10, 6}, []Rule{mon(1)}, nil, active, []TeamMember{}},
		{"rule ended the day before", monday,
			[]Rule{{MemberID: 1, DayOfWeek: 1, From: Date{2026, 1, 1}, To: ptr(Date{2026, 10, 4})}}, nil, active, []TeamMember{}},
		{"rule ends on the night itself", monday,
			[]Rule{{MemberID: 1, DayOfWeek: 1, From: Date{2026, 1, 1}, To: ptr(monday)}}, nil, active, sched(1)},
		{"rule starts the day after", monday,
			[]Rule{{MemberID: 1, DayOfWeek: 1, From: Date{2026, 10, 6}}}, nil, active, []TeamMember{}},
		{"rule starts on the night itself", monday,
			[]Rule{{MemberID: 1, DayOfWeek: 1, From: monday}}, nil, active, sched(1)},
		{"inactive member", monday, []Rule{mon(1), mon(5)}, nil, active, sched(1)},
		{"duplicate rules", monday, []Rule{mon(1), mon(1)}, nil, active, sched(1)},
		{"ADD other member", monday, []Rule{mon(1)}, []Override{{Action: "ADD", MemberID: 9}}, active,
			[]TeamMember{{1, SourceScheduled}, {9, SourceOverride}}},
		{"ADD already scheduled", monday, []Rule{mon(1)}, []Override{{Action: "ADD", MemberID: 1}}, active, sched(1)},
		{"REMOVE scheduled", monday, []Rule{mon(1), mon(2)}, []Override{{Action: "REMOVE", MemberID: 1}}, active, sched(2)},
		{"SWAP A to B", monday, []Rule{mon(1)}, []Override{{Action: "SWAP", MemberID: 1, ReplacementID: id(9)}}, active,
			[]TeamMember{{9, SourceOverride}}},
		{"SWAP onto scheduled member", monday, []Rule{mon(1), mon(2)},
			[]Override{{Action: "SWAP", MemberID: 1, ReplacementID: id(2)}}, active, sched(2)},
		{"SWAP without replacement only removes", monday, []Rule{mon(1)}, []Override{{Action: "SWAP", MemberID: 1}}, active, []TeamMember{}},
		{"overrides apply in order", monday, nil,
			[]Override{{Action: "ADD", MemberID: 9}, {Action: "REMOVE", MemberID: 9}, {Action: "ADD", MemberID: 4}}, active,
			[]TeamMember{{4, SourceOverride}}},
		{"unknown action ignored", monday, []Rule{mon(1)}, []Override{{Action: "NOPE", MemberID: 1}}, active, sched(1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveTeam(tc.night, tc.rules, tc.overrides, tc.active)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

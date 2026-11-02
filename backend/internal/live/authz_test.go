package live

import "testing"

func TestRoleAllowsAuthzHierarchy(t *testing.T) {
	cases := []struct {
		role     string
		relation authzRelation
		want     bool
	}{
		{"team_owner", authzView, true},
		{"team_owner", authzManage, true},
		{"team_admin", authzApprove, true},
		{"member", authzView, true},
		{"member", authzContribute, true},
		{"member", authzApprove, false},
		{"member", authzManage, false},
		{"", authzView, false},
		{"suspended", authzView, false},
	}
	for _, tc := range cases {
		if got := roleAllows(tc.role, tc.relation); got != tc.want {
			t.Fatalf("role=%q relation=%q got=%v want=%v", tc.role, tc.relation, got, tc.want)
		}
	}
}

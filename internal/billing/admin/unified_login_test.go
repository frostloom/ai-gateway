package admin

import "testing"

func TestResolveLoginRoleRejectsAmbiguousIdentity(t *testing.T) {
	cases := []struct {
		admin, user bool
		want        string
	}{{true, false, "admin"}, {false, true, "user"}, {false, false, ""}, {true, true, ""}}
	for _, tc := range cases {
		if got := resolveLoginRole(tc.admin, tc.user); got != tc.want {
			t.Fatalf("admin=%v user=%v: got %q", tc.admin, tc.user, got)
		}
	}
}

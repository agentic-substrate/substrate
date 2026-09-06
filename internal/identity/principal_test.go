package identity

import "testing"

func TestIsAdminOnlyHumanAdminTrust(t *testing.T) {
	cases := []struct {
		trust Trust
		want  bool
	}{
		{TrustHumanAdmin, true},
		{TrustHuman, false},
		{TrustAgentInteractive, false},
		{TrustAgentAutonomous, false},
	}
	for _, tc := range cases {
		p := Principal{Trust: tc.trust}
		if got := p.IsAdmin(); got != tc.want {
			t.Errorf("trust %q IsAdmin()=%v, want %v (EDD R25)", tc.trust, got, tc.want)
		}
	}
}

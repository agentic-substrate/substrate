package scope

import "testing"

// CANARY: deliberately failing test to prove the gate blocks a merge.
// Removed in the next commit on this branch.
func TestGateCanary(t *testing.T) {
	t.Fatal("canary: this test fails on purpose to prove the required check blocks merges")
}

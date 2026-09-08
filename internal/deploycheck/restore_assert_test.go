package deploycheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func restoreAssertBin(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "scripts", "restore-assert.sh")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("scripts/restore-assert.sh missing: %v", err)
	}
	return p
}

func runRestoreAssert(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(restoreAssertBin(t), args...) //nolint:gosec // argv is test-controlled
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// A restore that produces zero rows on any of memory, instruction, or audit
// must fail. Treating 0 as success is the whole failure mode this job exists
// to prevent: a backup that "restored" an empty database looks healthy.
func TestRestoreAssertFailsOnZeroRows(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "all_zero", args: []string{"0", "0", "0"}, want: "memory"},
		{name: "memory_zero", args: []string{"0", "12", "40"}, want: "memory"},
		{name: "instruction_zero", args: []string{"9", "0", "40"}, want: "instruction"},
		{name: "audit_zero", args: []string{"9", "12", "0"}, want: "audit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runRestoreAssert(t, tc.args...)
			if err == nil {
				t.Fatalf("restore-assert %v succeeded; a zero count must be a failure.\noutput:\n%s", tc.args, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("failure did not name %q:\n%s", tc.want, out)
			}
		})
	}
}

func TestRestoreAssertPassesWhenEveryTableHasRows(t *testing.T) {
	out, err := runRestoreAssert(t, "1", "1", "1")
	if err != nil {
		t.Fatalf("restore-assert 1 1 1 failed: %v\n%s", err, out)
	}
}

func TestRestoreAssertRejectsNonNumericCounts(t *testing.T) {
	out, err := runRestoreAssert(t, "", "1", "1")
	if err == nil {
		t.Fatalf("empty memory count succeeded; missing counts must fail.\n%s", out)
	}
	out, err = runRestoreAssert(t, "n/a", "1", "1")
	if err == nil {
		t.Fatalf("non-numeric memory count succeeded.\n%s", out)
	}
}

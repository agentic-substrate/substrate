package cutover

import (
	"strings"
	"testing"
)

func TestOSInstallerNilExecRefuses(t *testing.T) {
	// Falling back to exec.Command when Exec is nil is the one-line change
	// that makes this red — that would reach this machine's systemd.
	inst := OSInstaller{GOOS: "linux"}
	err := inst.Uninstall(UnitSpec{Home: t.TempDir(), Roots: []string{DefaultMountRoot}})
	if err == nil || !strings.Contains(err.Error(), "Exec is nil") {
		t.Fatalf("got %v, want nil Exec refused", err)
	}
	err = inst.Install(UnitSpec{Home: t.TempDir(), Server: "https://cp.example", Roots: []string{DefaultMountRoot}})
	if err == nil || !strings.Contains(err.Error(), "Exec is nil") {
		t.Fatalf("install got %v, want nil Exec refused", err)
	}
}

func TestOSInstallerUninstallUsesInjectedExec(t *testing.T) {
	// Calling exec.Command instead of o.Exec is the change that makes this red.
	home := t.TempDir()
	var ran []string
	inst := OSInstaller{
		GOOS: "linux",
		Exec: func(name string, args ...string) error {
			ran = append(ran, name+" "+strings.Join(args, " "))
			return nil
		},
	}
	if err := inst.Uninstall(UnitSpec{Home: home, Roots: []string{DefaultMountRoot}}); err != nil {
		t.Fatal(err)
	}
	if len(ran) == 0 {
		t.Fatal("Uninstall did not call Exec")
	}
	for _, c := range ran {
		if strings.Contains(c, "systemctl") && !strings.Contains(strings.Join(ran, "\n"), "--user") {
			t.Fatalf("systemctl without --user: %v", ran)
		}
	}
}

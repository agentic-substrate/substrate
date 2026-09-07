package cutover

import (
	"errors"
	"os"
	"path/filepath"
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

func TestOSInstallerInstallBacksUpExistingUnit(t *testing.T) {
	// os.WriteFile(O_TRUNC) over the live unit without renaming it to
	// *.pre-substrate is the one-line change that makes this red.
	home := t.TempDir()
	path := systemdUnitPath(home)
	original := "[Unit]\nDescription=pre-existing\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := OSInstaller{
		GOOS: "linux",
		Exec: func(string, ...string) error { return nil },
	}
	if err := inst.Install(UnitSpec{Home: home, Server: "https://cp.example", Roots: []string{DefaultMountRoot}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path + BackupSuffix) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatalf("existing unit was truncated with no *.pre-substrate backup: %v", err)
	}
	if string(got) != original {
		t.Fatalf("backup %q, want original", got)
	}
}

func TestOSInstallerEnableFailureRollsBackOrKeepsBackup(t *testing.T) {
	// Leaving a truncated unit after enable --now fails, with no backup, is
	// the change that makes this red. Uninstall must then be able to clean up.
	home := t.TempDir()
	path := systemdUnitPath(home)
	original := "[Unit]\nDescription=pre-existing\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := OSInstaller{
		GOOS: "linux",
		Exec: func(_ string, args ...string) error {
			if strings.Contains(strings.Join(args, " "), "enable") {
				return errors.New("injected: enable failed")
			}
			return nil
		},
	}
	err := inst.Install(UnitSpec{Home: home, Server: "https://cp.example", Roots: []string{DefaultMountRoot}})
	if err == nil {
		t.Fatal("Install succeeded despite injected enable failure")
	}
	backup, berr := os.ReadFile(path + BackupSuffix) //nolint:gosec // under t.TempDir
	live, lerr := os.ReadFile(path)                  //nolint:gosec // under t.TempDir
	hasBackup := berr == nil && string(backup) == original
	rolledBack := lerr == nil && string(live) == original
	if !hasBackup && !rolledBack {
		t.Fatalf("existing unit lost after enable failure; live=%v backup=%v", lerr, berr)
	}
	uninst := OSInstaller{
		GOOS: "linux",
		Exec: func(string, ...string) error {
			return errors.New("Failed to disable unit: Unit substrate-adapter.service not loaded.")
		},
	}
	if err := uninst.Uninstall(UnitSpec{Home: home, Roots: []string{DefaultMountRoot}}); err != nil {
		t.Fatalf("Uninstall after failed enable: %v", err)
	}
}

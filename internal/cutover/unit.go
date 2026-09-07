package cutover

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	systemdUnitName = "substrate-adapter.service"
	launchdLabel    = "com.agentic-substrate.adapter"
)

// UnitInstaller installs or removes the adapter unit. Tests inject a fake;
// production uses OSInstaller. Tests must not exec systemctl or launchctl.
type UnitInstaller interface {
	Install(spec UnitSpec) error
	Uninstall(spec UnitSpec) error
}

// UnitSpec is what the unit file / plist would contain. Roots is always
// DefaultMountRoot (/work) so CONT-4 holds even when tests use TempDir.
type UnitSpec struct {
	Binary  string
	Home    string
	Server  string
	Token   string
	Machine string
	Roots   []string
}

// FakeInstaller records calls and never touches systemd or launchd.
type FakeInstaller struct {
	Installs   []UnitSpec
	Uninstalls int
}

// Install records spec and returns nil.
func (f *FakeInstaller) Install(spec UnitSpec) error {
	if f == nil {
		return nil
	}
	f.Installs = append(f.Installs, spec)
	return nil
}

// Uninstall increments Uninstalls and returns nil.
func (f *FakeInstaller) Uninstall(_ UnitSpec) error {
	if f == nil {
		return nil
	}
	f.Uninstalls++
	return nil
}

// ExecFunc runs a subprocess. Tests inject a recorder; production uses
// exec.Command. A nil Exec on OSInstaller is an error, never a fallback to
// the real systemctl/launchctl — that would install on this machine.
type ExecFunc func(name string, args ...string) error

// OSInstaller writes a systemd --user unit or a launchd agent under spec.Home
// and optionally execs the supervisor. Tests must not construct this with
// NewOSInstaller; use FakeInstaller.
type OSInstaller struct {
	GOOS string
	Exec ExecFunc
}

// NewOSInstaller returns the production installer for this GOOS. Do not call
// this from tests.
func NewOSInstaller() UnitInstaller {
	return OSInstaller{GOOS: runtime.GOOS, Exec: defaultExec}
}

func defaultExec(name string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:gosec // name/args are our unit-helper literals
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cutover: %s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (o OSInstaller) goos() string {
	if o.GOOS != "" {
		return o.GOOS
	}
	return runtime.GOOS
}

func (o OSInstaller) exec(name string, args ...string) error {
	if o.Exec == nil {
		return fmt.Errorf("cutover: OSInstaller.Exec is nil; refusing to exec %s", name)
	}
	return o.Exec(name, args...)
}

// Install writes the unit file under spec.Home and execs the supervisor.
func (o OSInstaller) Install(spec UnitSpec) error {
	if spec.Home == "" {
		return fmt.Errorf("cutover: unit install requires Home")
	}
	if err := o.writeUnit(spec); err != nil {
		return err
	}
	switch o.goos() {
	case "darwin":
		return o.exec("launchctl", "load", launchdPlistPath(spec.Home))
	default:
		unit := systemdUnitPath(spec.Home)
		if err := o.exec("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return o.exec("systemctl", "--user", "enable", "--now", filepath.Base(unit))
	}
}

// Uninstall stops the unit and renames its file to *.pre-substrate.
func (o OSInstaller) Uninstall(spec UnitSpec) error {
	if spec.Home == "" {
		return fmt.Errorf("cutover: unit uninstall requires Home")
	}
	switch o.goos() {
	case "darwin":
		plist := launchdPlistPath(spec.Home)
		if err := o.exec("launchctl", "unload", plist); err != nil {
			return err
		}
		return retirePath(plist)
	default:
		if err := o.exec("systemctl", "--user", "disable", "--now", systemdUnitName); err != nil {
			return err
		}
		return retirePath(systemdUnitPath(spec.Home))
	}
}

func (o OSInstaller) writeUnit(spec UnitSpec) error {
	switch o.goos() {
	case "darwin":
		path := launchdPlistPath(spec.Home)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(LaunchdPlist(spec)), 0o600)
	default:
		path := systemdUnitPath(spec.Home)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(SystemdUnit(spec)), 0o600)
	}
}

func systemdUnitPath(home string) string {
	return filepath.Join(home, ".config", "systemd", "user", systemdUnitName)
}

func launchdPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

func retirePath(path string) error {
	_, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	backup := path + BackupSuffix
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("cutover: refusing to overwrite existing %s", backup)
	}
	return os.Rename(path, backup)
}

func adapterArgs(spec UnitSpec) []string {
	roots := spec.Roots
	if len(roots) == 0 {
		roots = []string{DefaultMountRoot}
	}
	bin := spec.Binary
	if bin == "" {
		bin = "substrate-adapter"
	}
	return []string{
		bin,
		"-server", spec.Server,
		"-token", spec.Token,
		"-home", spec.Home,
		"-roots", strings.Join(roots, ","),
		"-machine", spec.Machine,
	}
}

// SystemdUnit is the systemd --user unit body. It pins -roots /work (CONT-4)
// and an explicit -home; it never expands $HOME.
func SystemdUnit(spec UnitSpec) string {
	args := adapterArgs(spec)
	execStart := strings.Join(args, " ")
	return "[Unit]\n" +
		"Description=Substrate adapter\n" +
		"After=network-online.target\n" +
		"\n" +
		"[Service]\n" +
		"Type=simple\n" +
		"ExecStart=" + execStart + "\n" +
		"Restart=on-failure\n" +
		"\n" +
		"[Install]\n" +
		"WantedBy=default.target\n"
}

// LaunchdPlist is the launchd agent body. ProgramArguments include /work.
func LaunchdPlist(spec UnitSpec) string {
	args := adapterArgs(spec)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("  <key>Label</key>\n")
	b.WriteString("  <string>" + launchdLabel + "</string>\n")
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, a := range args {
		b.WriteString("    <string>" + xmlEscape(a) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	b.WriteString("  <key>KeepAlive</key>\n  <true/>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

package deploycheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func secretsScanner(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "scripts", "check-deploy-secrets.sh")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("scripts/check-deploy-secrets.sh missing: %v", err)
	}
	return p
}

func runSecretsScanner(t *testing.T, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command(secretsScanner(t), dir) //nolint:gosec // argv is test-controlled
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// A scanner that has never been shown a secret proves nothing. Seed a fake
// credential in a temp tree and require a non-zero exit that names the finding.
func TestDeploySecretScannerCatchesSeededPassword(t *testing.T) {
	dir := t.TempDir()
	leak := filepath.Join(dir, "leaky.yaml")
	if err := os.WriteFile(leak, []byte("apiVersion: v1\nkind: Secret\nstringData:\n  password: hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runSecretsScanner(t, dir)
	if err == nil {
		t.Fatalf("scanner accepted a seeded password; output:\n%s", out)
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "hunter2") && !strings.Contains(lower, "password") {
		t.Fatalf("failure did not name the seeded credential:\n%s", out)
	}
}

func TestDeploySecretScannerCatchesTailnetHostname(t *testing.T) {
	dir := t.TempDir()
	leak := filepath.Join(dir, "ingress.yaml")
	if err := os.WriteFile(leak, []byte("host: substrate.example.ts.net\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runSecretsScanner(t, dir)
	if err == nil {
		t.Fatalf("scanner accepted a ts.net hostname; output:\n%s", out)
	}
	if !strings.Contains(out, "ts.net") {
		t.Fatalf("failure did not name ts.net:\n%s", out)
	}
}

func TestDeploySecretScannerAllowsEmptyAndPlaceholderValues(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "secret.yaml")
	body := strings.Join([]string{
		"apiVersion: v1",
		"kind: Secret",
		"metadata:",
		"  name: substrate",
		"stringData:",
		"  password: \"\"",
		"  ACCESS_KEY_ID: \"\"",
		"  host: substrate.<tailnet>",
		"",
	}, "\n")
	if err := os.WriteFile(ok, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runSecretsScanner(t, dir)
	if err != nil {
		t.Fatalf("scanner rejected empty/placeholder values: %v\n%s", err, out)
	}
}

func TestDeployTreeContainsNoCredentialOrTailnet(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "deploy")
	out, err := runSecretsScanner(t, dir)
	if err != nil {
		t.Fatalf("deploy/ failed the secret scan: %v\n%s", err, out)
	}
}

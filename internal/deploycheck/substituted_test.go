package deploycheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The gate the runbook makes mandatory before `kubectl apply` (steps 2 and 6)
// had only ever been shown failing trees. It could not exit 0 on ANY tree: it
// tripped on `<empty>` inside an error-message string in restore-assert.sh, on
// the operator-side `mc` templates under deploy/minio/ (which can only be
// "substituted" by writing live MinIO keys into a file in the repo), and on
// ingress/certificate.yaml, which is deliberately not applied at all. The PR
// body read that exit 1 as correct behaviour. It was not: a mandatory gate
// that cannot pass is a step the operator learns to skip.
//
// This is the pass-path test. It builds the tree the operator is actually
// supposed to hand to `kubectl apply -k` and requires exit 0.

var placeholderRe = regexp.MustCompile(`<([a-z0-9][a-z0-9-]*)>`)

// Files the operator must NOT fill in the working tree, and which the scanner
// therefore must not demand. Kept in step with the scanner's own list on
// purpose: if they drift, this fixture stops resembling reality.
func operatorLeavesAlone(rel string) bool {
	return strings.HasPrefix(rel, "minio/") ||
		rel == "ingress/certificate.yaml" ||
		strings.HasSuffix(rel, ".sh")
}

const fixtureDigest = "registry.example.test/substituted@sha256:" +
	"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// substituteLikeAnOperator writes a copy of deploy/ with every placeholder
// filled the way runbook step 2 describes: images by digest, everything else
// by a plausible literal, and both Secrets populated.
func substituteLikeAnOperator(t *testing.T, dst string) {
	t.Helper()
	src := filepath.Join(repoRoot(t), "deploy")

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o750)
		}
		b, rerr := os.ReadFile(path) //nolint:gosec // repo file, test-controlled
		if rerr != nil {
			return rerr
		}
		body := string(b)
		if !operatorLeavesAlone(filepath.ToSlash(rel)) {
			body = placeholderRe.ReplaceAllStringFunc(body, func(m string) string {
				name := m[1 : len(m)-1]
				if strings.Contains(name, "image") || strings.Contains(name, "digest") {
					return fixtureDigest
				}
				return "substituted-" + name
			})
			body = fillSecrets(body)
		}
		return os.WriteFile(out, []byte(body), 0o600) //nolint:gosec // out is under t.TempDir()
	})
	if err != nil {
		t.Fatalf("build substituted fixture: %v", err)
	}
}

// `KEY: ""` is the correct committed state and the wrong applied state.
func fillSecrets(body string) string {
	empty := regexp.MustCompile(`(?m)^(\s*)(SUBSTRATE_DSN|ACCESS_KEY_ID|ACCESS_SECRET_KEY|POSTGRES_PASSWORD|password|token):\s*""\s*$`)
	return empty.ReplaceAllString(body, `${1}${2}: "filled-by-the-operator"`)
}

// Red when: the scanner rejects a tree the operator has correctly substituted
// — because it scans a file `kubectl apply -k` never applies, or because it
// reads a shell string as an operator work item.
func TestPlaceholderScannerPassesOnACorrectlySubstitutedTree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deploy")
	substituteLikeAnOperator(t, dir)

	out, err := runPlaceholders(t, "--substituted", dir)
	if err != nil {
		t.Fatalf("a correctly-substituted deploy/ must pass the gate the runbook makes mandatory "+
			"before apply, but it exited non-zero:\n%s", out)
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("expected an explicit OK, got:\n%s", out)
	}
}

// The pass case must not be pass-because-it-checks-nothing. Break exactly one
// thing in the otherwise-correct tree and the gate must still catch it.
func TestSubstitutedGateStillCatchesOneBadValueInAnOtherwiseCorrectTree(t *testing.T) {
	base := t.TempDir()

	t.Run("surviving placeholder", func(t *testing.T) {
		dir := filepath.Join(base, "a", "deploy")
		substituteLikeAnOperator(t, dir)
		f := filepath.Join(dir, "server", "configmap.yaml")
		b, err := os.ReadFile(f) //nolint:gosec // fixture path
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, append(b, []byte("\n  LEFTOVER: substrate.<tailnet>\n")...), 0o600); err != nil { //nolint:gosec // fixture under t.TempDir()
			t.Fatal(err)
		}
		out, err := runPlaceholders(t, "--substituted", dir)
		if err == nil {
			t.Fatalf("gate passed a tree with a surviving <tailnet>:\n%s", out)
		}
	})

	t.Run("image reverted to a tag", func(t *testing.T) {
		dir := filepath.Join(base, "b", "deploy")
		substituteLikeAnOperator(t, dir)
		f := filepath.Join(dir, "cnpg", "cluster.yaml")
		b, err := os.ReadFile(f) //nolint:gosec // fixture path
		if err != nil {
			t.Fatal(err)
		}
		body := strings.Replace(string(b), fixtureDigest, "substrate-pg:16-pgvector", 1)
		if err := os.WriteFile(f, []byte(body), 0o600); err != nil { //nolint:gosec // fixture under t.TempDir()
			t.Fatal(err)
		}
		out, err := runPlaceholders(t, "--substituted", dir)
		if err == nil {
			t.Fatalf("gate passed a mutable tag:\n%s", out)
		}
		if !strings.Contains(out, "not pinned by digest") {
			t.Fatalf("failure did not name the missing digest:\n%s", out)
		}
	})

	t.Run("secret left empty", func(t *testing.T) {
		dir := filepath.Join(base, "c", "deploy")
		substituteLikeAnOperator(t, dir)
		f := filepath.Join(dir, "server", "secret.yaml")
		b, err := os.ReadFile(f) //nolint:gosec // fixture path
		if err != nil {
			t.Fatal(err)
		}
		body := strings.Replace(string(b), `SUBSTRATE_DSN: "filled-by-the-operator"`, `SUBSTRATE_DSN: ""`, 1)
		if err := os.WriteFile(f, []byte(body), 0o600); err != nil { //nolint:gosec // fixture under t.TempDir()
			t.Fatal(err)
		}
		out, err := runPlaceholders(t, "--substituted", dir)
		if err == nil {
			t.Fatalf("gate passed an empty SUBSTRATE_DSN:\n%s", out)
		}
		if !strings.Contains(out, "still empty") {
			t.Fatalf("failure did not name the empty value:\n%s", out)
		}
	})
}

// Filling deploy/minio/* means putting live MinIO access and secret keys into a
// file in the working tree — the exact leak check-deploy-secrets.sh exists to
// stop. A gate that demands it manufactures the incident it is meant to
// prevent, so those files are out of scope for substituted mode and the
// runbook has to say so where the operator will read it.
func TestOperatorIsNeverToldToFillCredentialsIntoTheRepoTree(t *testing.T) {
	rb := string(readFile(t, "docs/ops/runbook.md"))
	if !strings.Contains(rb, "never filled in the repo tree") {
		t.Fatal("runbook step 3 must say the MinIO credentials are never filled in the repo tree")
	}
	if !strings.Contains(rb, "deploy/minio/") {
		t.Fatal("runbook step 2 must name deploy/minio/ as out of scope for the --substituted gate")
	}
	if !strings.Contains(rb, "must exit **0**") {
		t.Fatal("runbook must state that the --substituted gate is expected to pass, not to exit 1")
	}

	scanner := string(readFile(t, "scripts/check-deploy-placeholders.sh"))
	if !strings.Contains(scanner, "NOT_APPLIED_PREFIXES") {
		t.Fatal("scanner must scope substituted mode to what kubectl apply -k applies")
	}
}

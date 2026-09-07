package deploycheck

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func readFile(t *testing.T, rel string) []byte {
	t.Helper()
	p := filepath.Join(repoRoot(t), rel)
	b, err := os.ReadFile(p) //nolint:gosec // test reads repo files
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return b
}

func loadYAMLDocs(t *testing.T, rel string) []map[string]any {
	t.Helper()
	raw := readFile(t, rel)
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var docs []map[string]any
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("yaml %s: %v", rel, err)
		}
		if doc == nil {
			continue
		}
		docs = append(docs, doc)
	}
	if len(docs) == 0 {
		t.Fatalf("%s: no YAML documents", rel)
	}
	return docs
}

func asMap(t *testing.T, v any, path string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s: want map, got %T", path, v)
	}
	return m
}

func nested(t *testing.T, doc map[string]any, keys ...string) any {
	t.Helper()
	var cur any = doc
	path := ""
	for _, k := range keys {
		path += "." + k
		m := asMap(t, cur, path)
		next, ok := m[k]
		if !ok {
			t.Fatalf("%s: missing", path)
		}
		cur = next
	}
	return cur
}

func str(t *testing.T, v any, path string) string {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("%s: want string, got %T (%v)", path, v, v)
	}
	return s
}

func TestRequiredDeployFilesExist(t *testing.T) {
	files := []string{
		"deploy/namespace.yaml",
		"deploy/kustomization.yaml",
		"deploy/cnpg/cluster.yaml",
		"deploy/cnpg/scheduled-backup.yaml",
		"deploy/cnpg/scratch-cluster.yaml",
		"deploy/cnpg/Dockerfile",
		"deploy/server/deployment.yaml",
		"deploy/server/service.yaml",
		"deploy/server/configmap.yaml",
		"deploy/server/secret.yaml",
		"deploy/server/pdb.yaml",
		"deploy/networkpolicy.yaml",
		"deploy/otel/collector.yaml",
		"deploy/cnpg/backups-secret.yaml",
		"deploy/ingress/middleware.yaml",
		"deploy/ingress/certificate.yaml",
		"deploy/ingress/ingressroute.yaml",
		"deploy/restore/cronjob.yaml",
		"deploy/restore/rbac.yaml",
		"deploy/restore/restore-test.sh",
		"deploy/restore/restore-assert.sh",
		"deploy/minio/buckets.sh",
		"scripts/check-deploy-placeholders.sh",
		"deploy/minio/session-policy.json",
		".ko.yaml",
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(repoRoot(t), f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
}

func TestKubernetesManifestsHaveIdentityAndNamespace(t *testing.T) {
	root := filepath.Join(repoRoot(t), "deploy")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot(t), path)
		raw, readErr := os.ReadFile(path) //nolint:gosec // walking deploy/
		if readErr != nil {
			t.Errorf("read %s: %v", rel, readErr)
			return nil
		}
		if !bytes.Contains(raw, []byte("apiVersion:")) {
			return nil
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		n := 0
		for {
			var doc map[string]any
			if decErr := dec.Decode(&doc); decErr != nil {
				if decErr.Error() == "EOF" {
					break
				}
				t.Errorf("yaml %s: %v", rel, decErr)
				return nil
			}
			if doc == nil {
				continue
			}
			n++
			kind := str(t, doc["kind"], rel+".kind")
			if doc["apiVersion"] == nil || str(t, doc["apiVersion"], rel+".apiVersion") == "" {
				t.Errorf("%s doc %d: missing apiVersion", rel, n)
			}
			meta := asMap(t, doc["metadata"], rel+".metadata")
			if str(t, meta["name"], rel+".metadata.name") == "" {
				t.Errorf("%s doc %d: missing metadata.name", rel, n)
			}
			if kind == "Namespace" || kind == "Kustomization" {
				continue
			}
			ns, _ := meta["namespace"].(string)
			if ns == "" {
				t.Errorf("%s doc %d kind %s: missing metadata.namespace", rel, n, kind)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresClusterIsSingleInstanceLonghornWithRoles(t *testing.T) {
	docs := loadYAMLDocs(t, "deploy/cnpg/cluster.yaml")
	var cluster map[string]any
	for _, d := range docs {
		if d["kind"] == "Cluster" {
			cluster = d
			break
		}
	}
	if cluster == nil {
		t.Fatal("deploy/cnpg/cluster.yaml has no Cluster")
	}
	if str(t, nested(t, cluster, "metadata", "name"), "name") != "substrate-pg" {
		t.Fatalf("cluster name %v, want substrate-pg", nested(t, cluster, "metadata", "name"))
	}
	spec := asMap(t, cluster["spec"], "spec")
	switch v := spec["instances"].(type) {
	case int:
		if v != 1 {
			t.Fatalf("instances %d, want 1 (EDD §1.1)", v)
		}
	case int64:
		if v != 1 {
			t.Fatalf("instances %d, want 1 (EDD §1.1)", v)
		}
	default:
		t.Fatalf("instances type %T (%v), want 1", spec["instances"], spec["instances"])
	}
	storage := asMap(t, spec["storage"], "spec.storage")
	size := str(t, storage["size"], "spec.storage.size")
	if size == "" {
		t.Fatal("storage.size must be explicit; a Longhorn default can starve a neighbour")
	}
	if str(t, storage["storageClass"], "spec.storage.storageClass") != "longhorn" {
		t.Fatalf("storageClass %v, want longhorn", storage["storageClass"])
	}
	raw := string(readFile(t, "deploy/cnpg/cluster.yaml"))
	for _, ext := range []string{"vector", "pg_trgm", "ltree"} {
		if !strings.Contains(raw, ext) {
			t.Errorf("cluster manifest does not name extension %s", ext)
		}
	}
	if !strings.Contains(raw, "substrate_migrate") || !strings.Contains(raw, "substrate_app") {
		t.Fatal("cluster must declare roles substrate_migrate and substrate_app")
	}
	if !strings.Contains(strings.ToLower(raw), "bypassrls") {
		t.Fatal("cluster must set bypassrls false on substrate_app")
	}
	if strings.Contains(raw, "BYPASSRLS: true") || strings.Contains(raw, "bypassrls: true") {
		t.Fatal("substrate_app must not have BYPASSRLS")
	}
}

func TestBarmanCloudBackupToMinIO(t *testing.T) {
	raw := string(readFile(t, "deploy/cnpg/cluster.yaml"))
	if !strings.Contains(raw, "substrate-backups") {
		t.Fatal("cluster backup destination must be bucket substrate-backups")
	}
	if !strings.Contains(raw, "30d") {
		t.Fatal("backup retention must be 30d")
	}
	if !strings.Contains(raw, "AES256") {
		t.Fatal("barman-cloud must set SSE (AES256)")
	}
	sched := string(readFile(t, "deploy/cnpg/scheduled-backup.yaml"))
	if !strings.Contains(sched, "0 0 3") && !strings.Contains(sched, "0 3 *") {
		t.Fatal("ScheduledBackup must run daily at 03:00")
	}
}

func TestServerDeploymentReadyzOneReplica(t *testing.T) {
	docs := loadYAMLDocs(t, "deploy/server/deployment.yaml")
	dep := docs[0]
	if dep["kind"] != "Deployment" {
		t.Fatalf("kind %v, want Deployment", dep["kind"])
	}
	spec := asMap(t, dep["spec"], "spec")
	switch v := spec["replicas"].(type) {
	case int:
		if v != 1 {
			t.Fatalf("replicas %d, want 1", v)
		}
	case int64:
		if v != 1 {
			t.Fatalf("replicas %d, want 1", v)
		}
	default:
		t.Fatalf("replicas type %T (%v), want 1", spec["replicas"], spec["replicas"])
	}
	raw := string(readFile(t, "deploy/server/deployment.yaml"))
	if !strings.Contains(raw, "/readyz") {
		t.Fatal("readiness probe must be /readyz")
	}
	if strings.Contains(raw, "path: /healthz") && strings.Contains(raw, "readinessProbe") {
		// liveness may use /healthz; readiness must not
		if i := strings.Index(raw, "readinessProbe"); i >= 0 {
			chunk := raw[i:]
			if j := strings.Index(chunk, "livenessProbe"); j > 0 {
				chunk = chunk[:j]
			}
			if strings.Contains(chunk, "/healthz") {
				t.Fatal("readinessProbe must not use /healthz (alive ≠ ready)")
			}
		}
	}
	svc := string(readFile(t, "deploy/server/service.yaml"))
	if strings.Contains(svc, "LoadBalancer") {
		t.Fatal("Service must not be LoadBalancer; Tailscale is the only path")
	}
}

func TestIngressRouteIsTailnetOnlyWithMCPRateLimit(t *testing.T) {
	ir := string(readFile(t, "deploy/ingress/ingressroute.yaml"))
	if !strings.Contains(ir, "substrate.<tailnet>") {
		t.Fatal("IngressRoute host must be substrate.<tailnet>")
	}
	if strings.Contains(ir, "ts.net") {
		t.Fatal("IngressRoute must not embed a real tailnet name")
	}
	if !strings.Contains(ir, "/mcp") || !strings.Contains(ir, "/v1") {
		t.Fatal("IngressRoute must route /mcp and /v1")
	}
	mw := string(readFile(t, "deploy/ingress/middleware.yaml"))
	if !strings.Contains(mw, "rateLimit") {
		t.Fatal("missing RateLimit middleware")
	}
	if !strings.Contains(mw, "Authorization") {
		t.Fatal("rate limit must key on Authorization (per-token)")
	}
	// The previous form of this assertion was `!A || (!B && !C)` by Go
	// precedence, with all three substrings present — always false, so the
	// t.Fatal was unreachable and deleting the middleware still passed.
	// Parse the route instead of grepping the file: the middleware must be
	// attached to the route that matches /mcp specifically, not merely be
	// mentioned somewhere in the document.
	mwName := middlewareName(t)
	route := mcpRoute(t)
	attached, _ := route["middlewares"].([]any)
	if len(attached) == 0 {
		t.Fatalf("IngressRoute /mcp route has no middlewares; %s must be attached", mwName)
	}
	found := false
	for _, m := range attached {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := mm["name"].(string); n == mwName {
			found = true
		}
	}
	if !found {
		t.Fatalf("IngressRoute /mcp does not attach middleware %q; attached=%v", mwName, attached)
	}
	// /v1 is deliberately unlimited. Assert the decision is written down, so
	// it stays a choice rather than decaying into an omission nobody notices.
	if v1 := v1Route(t); len(v1) > 0 {
		if mws, _ := v1["middlewares"].([]any); len(mws) > 0 {
			t.Logf("/v1 now has middlewares %v — update the comment explaining why it was unlimited", mws)
		}
	}
	if !strings.Contains(ir, "/v1` has NO rate limit") && !strings.Contains(ir, "NO rate limit") {
		t.Fatal("ingressroute.yaml must state why /v1 has no rate limit; an unexplained omission is indistinguishable from a mistake")
	}
}

// middlewareName returns the single Middleware object's metadata.name.
func middlewareName(t *testing.T) string {
	t.Helper()
	for _, d := range loadYAMLDocs(t, "deploy/ingress/middleware.yaml") {
		if d["kind"] == "Middleware" {
			return str(t, nested(t, d, "metadata", "name"), "middleware.metadata.name")
		}
	}
	t.Fatal("deploy/ingress/middleware.yaml has no Middleware")
	return ""
}

// routeMatching returns the IngressRoute route whose match contains want.
func routeMatching(t *testing.T, want string) map[string]any {
	t.Helper()
	for _, d := range loadYAMLDocs(t, "deploy/ingress/ingressroute.yaml") {
		if d["kind"] != "IngressRoute" {
			continue
		}
		routes, _ := nested(t, d, "spec", "routes").([]any)
		for _, r := range routes {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			if m, _ := rm["match"].(string); strings.Contains(m, want) {
				return rm
			}
		}
	}
	return nil
}

func mcpRoute(t *testing.T) map[string]any {
	t.Helper()
	r := routeMatching(t, "/mcp")
	if r == nil {
		t.Fatal("IngressRoute has no route matching PathPrefix(`/mcp`)")
	}
	return r
}

func v1Route(t *testing.T) map[string]any {
	t.Helper()
	return routeMatching(t, "/v1")
}

func TestSecretTemplateHasEmptyValues(t *testing.T) {
	docs := loadYAMLDocs(t, "deploy/server/secret.yaml")
	found := false
	for _, d := range docs {
		if d["kind"] != "Secret" {
			continue
		}
		found = true
		data, _ := d["stringData"].(map[string]any)
		if data == nil {
			t.Fatal("Secret must use stringData with named empty keys")
		}
		for k, v := range data {
			s, _ := v.(string)
			if s != "" && !strings.Contains(s, "<") {
				t.Errorf("Secret key %s has a non-empty non-placeholder value", k)
			}
		}
		// The server's secret carries the DSN and NOTHING else. The MinIO
		// backup keys live in deploy/cnpg/backups-secret.yaml so that an RCE
		// in substrate-server cannot read the backup bucket out of its own
		// environment. cmd/substrate-server never reads them.
		if _, ok := data["SUBSTRATE_DSN"]; !ok {
			t.Error("Secret missing key SUBSTRATE_DSN")
		}
		for _, k := range []string{"ACCESS_KEY_ID", "ACCESS_SECRET_KEY"} {
			if _, ok := data[k]; ok {
				t.Errorf("MinIO key %s must not be in the server Secret; it belongs in substrate-backups-s3", k)
			}
		}
	}
	if !found {
		t.Fatal("deploy/server/secret.yaml has no Secret")
	}
}

func TestNoAvailabilityHA(t *testing.T) {
	root := filepath.Join(repoRoot(t), "deploy")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, readErr := os.ReadFile(path) //nolint:gosec // walking deploy/
		if readErr != nil {
			return readErr
		}
		// A PodDisruptionBudget is not necessarily HA. `maxUnavailable: 0` is
		// the opposite: it makes a voluntary disruption (a node drain) block
		// and say so, instead of silently evicting the only replica of a
		// single-instance database. What must not appear is a PDB that
		// *asserts* redundancy — minAvailable > 1 or maxUnavailable > 0.
		if bytes.Contains(raw, []byte("kind: PodDisruptionBudget")) {
			for _, bad := range []string{"minAvailable: 2", "minAvailable: 3", "maxUnavailable: 1", "maxUnavailable: 2"} {
				if bytes.Contains(raw, []byte(bad)) {
					t.Errorf("%s: PDB %q implies more than one replica; one replica is the design", path, bad)
				}
			}
		}
		if bytes.Contains(raw, []byte("type: LoadBalancer")) {
			t.Errorf("%s: LoadBalancer would expose a public IP", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestScratchClusterIsRecoveryNotDefaultKustomize(t *testing.T) {
	raw := string(readFile(t, "deploy/cnpg/scratch-cluster.yaml"))
	if !strings.Contains(raw, "recovery") {
		t.Fatal("scratch cluster must bootstrap from recovery")
	}
	if !strings.Contains(raw, "substrate-pg-scratch") {
		t.Fatal("scratch cluster name must be substrate-pg-scratch")
	}
	if strings.Contains(raw, "name: substrate-pg\n") && !strings.Contains(raw, "substrate-pg-scratch") {
		t.Fatal("scratch cluster must not reuse the production name")
	}
	if !strings.Contains(raw, "size:") {
		t.Fatal("scratch PVC size must be explicit")
	}
	docs := loadYAMLDocs(t, "deploy/kustomization.yaml")
	res, _ := docs[0]["resources"].([]any)
	for _, r := range res {
		s, _ := r.(string)
		if strings.Contains(s, "scratch-cluster.yaml") {
			t.Fatal("kustomize resources must not apply the scratch cluster by default")
		}
	}
}

func TestWeeklyRestoreCronJobFailsLoudly(t *testing.T) {
	cron := string(readFile(t, "deploy/restore/cronjob.yaml"))
	if !strings.Contains(cron, "kind: CronJob") {
		t.Fatal("missing CronJob")
	}
	script := string(readFile(t, "deploy/restore/restore-test.sh"))
	if !strings.Contains(script, "restore-assert.sh") {
		t.Fatal("restore-test.sh must invoke restore-assert.sh")
	}
	for _, table := range []string{"memory", "instruction", "audit"} {
		if !strings.Contains(script, table) {
			t.Errorf("restore-test.sh does not count %s", table)
		}
	}
	if !strings.Contains(script, "scratch") {
		t.Fatal("restore-test.sh must restore into a scratch cluster")
	}
}

func TestMinIOBucketCommandsAreDocumentedNotLiveCredentials(t *testing.T) {
	sh := string(readFile(t, "deploy/minio/buckets.sh"))
	if !strings.Contains(sh, "substrate-backups") || !strings.Contains(sh, "substrate-sessions") {
		t.Fatal("minio buckets.sh must name both buckets")
	}
	if !strings.Contains(sh, "mc ") {
		t.Fatal("minio buckets.sh must be an mc command list")
	}
	pol := string(readFile(t, "deploy/minio/session-policy.json"))
	if !strings.Contains(pol, "Deny") && !strings.Contains(pol, "deny") {
		t.Fatal("session-policy.json must be owner-only (deny others)")
	}
}

func TestRunbookKeepsGooseDownWarningAndOperatorChecklist(t *testing.T) {
	raw := string(readFile(t, "docs/ops/runbook.md"))
	lower := strings.ToLower(raw)
	if !strings.Contains(lower, "goose down") {
		t.Fatal("runbook must keep saying never goose down against this database")
	}
	if !strings.Contains(lower, "cannot be rebuilt") {
		t.Fatal("runbook must keep the audit-table wording")
	}
	if !strings.Contains(lower, "blast radius") {
		t.Fatal("runbook operator checklist must state blast radius per step")
	}
	if !strings.Contains(raw, "Requires the operator") && !strings.Contains(raw, "Operator checklist") {
		t.Fatal("runbook must include an ordered operator checklist")
	}
}

func TestKoDistrolessBase(t *testing.T) {
	raw := string(readFile(t, ".ko.yaml"))
	if !strings.Contains(raw, "distroless") {
		t.Fatal(".ko.yaml must use a distroless base")
	}
}

func TestCNPGDockerfileHasPgvector(t *testing.T) {
	raw := string(readFile(t, "deploy/cnpg/Dockerfile"))
	if !strings.Contains(raw, "pgvector") && !strings.Contains(raw, "postgresql-16-pgvector") {
		t.Fatal("CNPG Dockerfile must install pgvector")
	}
	if !strings.Contains(raw, "cloudnative-pg/postgresql") {
		t.Fatal("CNPG Dockerfile must start from the CNPG postgres image")
	}
}

func TestCronScheduleIsWeekly(t *testing.T) {
	raw := string(readFile(t, "deploy/restore/cronjob.yaml"))
	// Kubernetes CronJob is 5-field. Weekly Sunday 04:00: "0 4 * * 0"
	if !strings.Contains(raw, "0 4 * * 0") && !strings.Contains(raw, "0 4 * * 7") {
		t.Fatal("CronJob schedule must be weekly")
	}
}

func TestInstancesStayOne(t *testing.T) {
	raw := string(readFile(t, "deploy/cnpg/cluster.yaml"))
	if strings.Contains(raw, "instances: 3") || strings.Contains(raw, "instances: 2") {
		t.Fatal("production cluster must be one instance")
	}
}

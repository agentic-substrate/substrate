package deploycheck

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// docOfKind returns the first document of the given kind in rel.
func docOfKind(t *testing.T, rel, kind string) map[string]any {
	t.Helper()
	for _, d := range loadYAMLDocs(t, rel) {
		if d["kind"] == kind {
			return d
		}
	}
	t.Fatalf("%s has no %s", rel, kind)
	return nil
}

func docsOfKind(t *testing.T, rel, kind string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, d := range loadYAMLDocs(t, rel) {
		if d["kind"] == kind {
			out = append(out, d)
		}
	}
	return out
}

func boolAt(t *testing.T, doc map[string]any, keys ...string) bool {
	t.Helper()
	v := nested(t, doc, keys...)
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("%s: want bool, got %T (%v)", strings.Join(keys, "."), v, v)
	}
	return b
}

// The CronJob deletes and recreates a CNPG Cluster unattended, once a week.
// `kubectl apply -k deploy/` at runbook step 8 arms whatever is committed here,
// so if this manifest is not suspended, an apply on a Saturday fires Sunday
// 04:00 having never been verified by a human — the exact thing runbook step 10
// and issue #27 constraint 2 forbid. Step 11 unsuspends it deliberately, at
// which point the cluster differs from git on purpose.
//
// Red when: `suspend: true` is deleted from deploy/restore/cronjob.yaml, or
// flipped to false.
func TestRestoreCronJobShipsSuspended(t *testing.T) {
	cj := docOfKind(t, "deploy/restore/cronjob.yaml", "CronJob")
	spec := asMap(t, cj["spec"], "spec")
	v, ok := spec["suspend"]
	if !ok {
		t.Fatal("CronJob has no spec.suspend; the committed manifest must ship suspended so `kubectl apply -k deploy/` cannot arm an unverified weekly restore (runbook step 11 unsuspends it)")
	}
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("spec.suspend is %T (%v), want bool true", v, v)
	}
	if !b {
		t.Fatal("spec.suspend is false; the committed manifest must ship suspended (runbook step 11 patches it to false after the first hand restore)")
	}
	if !strings.Contains(string(readFile(t, "docs/ops/runbook.md")), `{"spec":{"suspend":false}}`) {
		t.Fatal("runbook step 11 must give the patch that arms the CronJob; a suspended job nobody knows to unsuspend is a backup that is never tested")
	}
}

// The script's own waits total 40 minutes. With concurrencyPolicy: Forbid and
// backoffLimit: 0, a hung Job stays Active forever and every following week is
// silently skipped — the job stops testing the backup and never says so.
//
// Red when: activeDeadlineSeconds is removed from the jobTemplate.
func TestRestoreJobHasActiveDeadline(t *testing.T) {
	cj := docOfKind(t, "deploy/restore/cronjob.yaml", "CronJob")
	js := asMap(t, nested(t, cj, "spec", "jobTemplate", "spec"), "jobTemplate.spec")
	v, ok := js["activeDeadlineSeconds"]
	if !ok {
		t.Fatal("jobTemplate.spec.activeDeadlineSeconds missing: with concurrencyPolicy Forbid a hung restore blocks every later week in silence")
	}
	n, ok := v.(int)
	if !ok {
		t.Fatalf("activeDeadlineSeconds is %T (%v), want int", v, v)
	}
	// The script waits 1800s for the Cluster then 600s for the pod.
	if n <= 2400 {
		t.Fatalf("activeDeadlineSeconds %d must exceed the script's own 2400s of waiting, or a healthy slow restore is killed", n)
	}
	if n > 21600 {
		t.Fatalf("activeDeadlineSeconds %d is longer than the gap to the next weekly run is useful for", n)
	}
}

// Every pod without requests is BestEffort QoS, which is the first thing the
// kubelet evicts under node memory pressure — on a single-node homelab that
// means substrate-server dies to save a neighbouring workload.
//
// Red when: the `resources:` block is deleted from any of the three manifests.
func TestWorkloadsDeclareResources(t *testing.T) {
	t.Run("server", func(t *testing.T) {
		dep := docOfKind(t, "deploy/server/deployment.yaml", "Deployment")
		cs, _ := nested(t, dep, "spec", "template", "spec", "containers").([]any)
		if len(cs) == 0 {
			t.Fatal("deployment has no containers")
		}
		for _, c := range cs {
			cm := asMap(t, c, "container")
			assertResources(t, cm, "server")
		}
	})
	t.Run("restoreJob", func(t *testing.T) {
		cj := docOfKind(t, "deploy/restore/cronjob.yaml", "CronJob")
		cs, _ := nested(t, cj, "spec", "jobTemplate", "spec", "template", "spec", "containers").([]any)
		if len(cs) == 0 {
			t.Fatal("cronjob has no containers")
		}
		for _, c := range cs {
			assertResources(t, asMap(t, c, "container"), "restore")
		}
	})
	t.Run("otelCollector", func(t *testing.T) {
		dep := docOfKind(t, "deploy/otel/collector.yaml", "Deployment")
		cs, _ := nested(t, dep, "spec", "template", "spec", "containers").([]any)
		for _, c := range cs {
			assertResources(t, asMap(t, c, "container"), "otel")
		}
	})
	// CNPG takes resources on the Cluster spec, not on a pod template.
	for _, rel := range []string{"deploy/cnpg/cluster.yaml", "deploy/cnpg/scratch-cluster.yaml"} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			cl := docOfKind(t, rel, "Cluster")
			res, ok := asMap(t, cl["spec"], "spec")["resources"]
			if !ok {
				t.Fatalf("%s: spec.resources missing; an unbounded barman-cloud restore has no ceiling on a node running other workloads", rel)
			}
			assertRequestsAndMemoryLimit(t, asMap(t, res, "spec.resources"), rel)
		})
	}
}

func assertResources(t *testing.T, container map[string]any, who string) {
	t.Helper()
	res, ok := container["resources"]
	if !ok {
		t.Fatalf("%s container %v: no resources; BestEffort QoS is evicted first under node memory pressure", who, container["name"])
	}
	assertRequestsAndMemoryLimit(t, asMap(t, res, who+".resources"), who)
}

// Memory is incompressible: a request without a limit still lets the container
// take the node down, and a limit without a request still leaves the pod in a
// QoS class the kubelet evicts early. Both, and they must match.
func assertRequestsAndMemoryLimit(t *testing.T, res map[string]any, who string) {
	t.Helper()
	reqs := asMap(t, res["requests"], who+".requests")
	lims := asMap(t, res["limits"], who+".limits")
	for _, k := range []string{"cpu", "memory"} {
		if v, ok := reqs[k]; !ok || v == "" {
			t.Errorf("%s: resources.requests.%s missing", who, k)
		}
	}
	memReq, _ := reqs["memory"].(string)
	memLim, _ := lims["memory"].(string)
	if memLim == "" {
		t.Errorf("%s: resources.limits.memory missing; memory is incompressible and an unbounded pod can take the node", who)
	}
	if memReq != memLim {
		t.Errorf("%s: memory request %q != limit %q; equal values are what make this Guaranteed rather than the kubelet's first eviction candidate", who, memReq, memLim)
	}
	// CPU limits are deliberately absent — CFS throttling adds tail latency on
	// a box that is not CPU-bound. Assert the absence so a future edit is a
	// decision, not a reflex.
	if _, ok := lims["cpu"]; ok && who == "server" {
		t.Errorf("%s: a CPU limit reintroduces CFS throttling; the request is what reserves capacity", who)
	}
}

// Red when: runAsNonRoot is removed from the server pod's securityContext.
func TestServerRunsAsNonRoot(t *testing.T) {
	dep := docOfKind(t, "deploy/server/deployment.yaml", "Deployment")
	if !boolAt(t, dep, "spec", "template", "spec", "securityContext", "runAsNonRoot") {
		t.Fatal("pod securityContext.runAsNonRoot must be true")
	}
	cs, _ := nested(t, dep, "spec", "template", "spec", "containers").([]any)
	c := asMap(t, cs[0], "container")
	sc := asMap(t, c["securityContext"], "container.securityContext")
	if b, _ := sc["allowPrivilegeEscalation"].(bool); b {
		t.Error("allowPrivilegeEscalation must be false")
	}
	if b, _ := sc["readOnlyRootFilesystem"].(bool); !b {
		t.Error("readOnlyRootFilesystem must be true")
	}
}

// The restore ServiceAccount previously held create/update/patch/delete on
// postgresql.cnpg.io/clusters with no resourceNames, plus unrestricted
// pods/exec — i.e. `kubectl delete cluster substrate-pg` (and CNPG takes the
// 20Gi Longhorn PVC with it), and `psql -U postgres` into production, from a
// weekly cron container whose NAMESPACE comes from env.
//
// Red when: any resourceNames list is removed from the delete/exec rules, or
// "substrate-pg" (production) is added to one.
func TestRestoreRBACCannotTouchProduction(t *testing.T) {
	const scratch = "substrate-pg-scratch"
	role := docOfKind(t, "deploy/restore/rbac.yaml", "Role")
	rules, _ := role["rules"].([]any)
	if len(rules) == 0 {
		t.Fatal("Role has no rules")
	}
	// Verbs that can destroy or read through the database. `create` is exempt
	// because RBAC resourceNames cannot constrain it: a create request has no
	// name to match, so such a rule would never fire.
	destructive := map[string]bool{"delete": true, "deletecollection": true, "update": true, "patch": true, "get": true}
	sawScopedDelete, sawScopedExec := false, false

	for i, r := range rules {
		rule := asMap(t, r, "rules[]")
		names := toStrings(rule["resourceNames"])
		for _, n := range names {
			if !strings.HasPrefix(n, scratch) {
				t.Errorf("rule %d names %q: the restore job must never be granted anything on production (%s*) only", i, n, scratch)
			}
		}
		verbs := toStrings(rule["verbs"])
		resources := toStrings(rule["resources"])
		for _, res := range resources {
			for _, v := range verbs {
				isExec := res == "pods/exec"
				if !destructive[v] && !isExec {
					continue
				}
				if len(names) == 0 {
					t.Errorf("rule %d grants %q on %q with no resourceNames; a typo, an edited ConfigMap, or a compromised image turns this into `delete cluster substrate-pg`", i, v, res)
					continue
				}
				if res == "clusters" && v == "delete" {
					sawScopedDelete = true
				}
				if isExec {
					sawScopedExec = true
				}
			}
		}
	}
	if !sawScopedDelete {
		t.Error("no name-scoped delete on clusters; the job still has to be able to remove its own scratch cluster")
	}
	if !sawScopedExec {
		t.Error("no name-scoped pods/exec; exec is `psql -U postgres`, a full RLS and audit bypass, and must be pinned to the scratch pod")
	}
}

func toStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, i := range items {
		if s, ok := i.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Issue #27 constraint 6 is "Tailscale is the only path". Without a
// NetworkPolicy that is only true north-south: any pod in the cluster could
// dial substrate-server:8080 and skip Traefik, the tailnet entrypoint, and the
// /mcp rate limit, or open a raw connection to substrate-pg-rw:5432.
//
// Red when: the default-deny document is removed from
// deploy/networkpolicy.yaml, or the file is dropped from kustomization.yaml.
func TestNetworkPolicyDefaultDeniesIngress(t *testing.T) {
	pols := docsOfKind(t, "deploy/networkpolicy.yaml", "NetworkPolicy")
	if len(pols) == 0 {
		t.Fatal("deploy/networkpolicy.yaml declares no NetworkPolicy")
	}
	defaultDeny := false
	for _, p := range pols {
		spec := asMap(t, p["spec"], "spec")
		sel, _ := spec["podSelector"].(map[string]any)
		types := toStrings(spec["policyTypes"])
		hasIngress := false
		for _, ty := range types {
			if ty == "Ingress" {
				hasIngress = true
			}
		}
		// Empty podSelector = every pod in the namespace; no ingress rules =
		// deny all. That pair is the default-deny.
		if len(sel) == 0 && hasIngress && spec["ingress"] == nil {
			defaultDeny = true
		}
	}
	if !defaultDeny {
		t.Fatal("no default-deny ingress policy (empty podSelector, policyTypes: [Ingress], no ingress rules); without it every pod in the cluster can bypass Traefik and the /mcp rate limit")
	}

	// Something must explicitly re-allow Traefik, or the app is unreachable.
	raw := string(readFile(t, "deploy/networkpolicy.yaml"))
	if !strings.Contains(raw, "<traefik-namespace>") {
		t.Error("no allow-from-Traefik rule; default-deny alone black-holes the IngressRoute")
	}

	res := toStrings(docOfKind(t, "deploy/kustomization.yaml", "Kustomization")["resources"])
	found := false
	for _, r := range res {
		if strings.Contains(r, "networkpolicy.yaml") {
			found = true
		}
	}
	if !found {
		t.Error("networkpolicy.yaml is not in kustomization resources, so `kubectl apply -k deploy/` never applies it")
	}
}

// A `maxUnavailable: 0` PDB is how an accidental `kubectl drain` is stopped
// from evicting the only replica of a single-instance database.
//
// Red when: deploy/server/pdb.yaml is deleted or dropped from kustomization.
func TestPodDisruptionBudgetsBlockAccidentalDrain(t *testing.T) {
	pdbs := docsOfKind(t, "deploy/server/pdb.yaml", "PodDisruptionBudget")
	if len(pdbs) < 2 {
		t.Fatalf("want a PDB for both substrate-server and substrate-pg, got %d", len(pdbs))
	}
	for _, p := range pdbs {
		name := str(t, nested(t, p, "metadata", "name"), "pdb.metadata.name")
		spec := asMap(t, p["spec"], "spec")
		v, ok := spec["maxUnavailable"]
		if !ok {
			t.Errorf("%s: want maxUnavailable: 0", name)
			continue
		}
		// Assert the type explicitly. `if n, _ := v.(int); n != 0` silently
		// passes when the decoder hands back an int64/uint64, which is how a
		// test that cannot fail gets written.
		var n int
		switch x := v.(type) {
		case int:
			n = x
		case int64:
			n = int(x)
		case uint64:
			if x > math.MaxInt32 {
				t.Errorf("%s: maxUnavailable %d is absurd", name, x)
				continue
			}
			n = int(x)
		default:
			t.Errorf("%s: maxUnavailable is %T (%v), want an integer", name, v, v)
			continue
		}
		if n != 0 {
			t.Errorf("%s: maxUnavailable %d, want 0 — anything higher permits evicting the single replica", name, n)
		}
	}
}

// The MinIO backup credentials must not be in substrate-server's environment:
// cmd/substrate-server never reads them, and an RCE there would otherwise hand
// out the backup bucket.
//
// Red when: `secretRef: name: substrate` (the combined secret) is restored in
// deploy/server/deployment.yaml.
func TestServerDoesNotReceiveBackupCredentials(t *testing.T) {
	dep := docOfKind(t, "deploy/server/deployment.yaml", "Deployment")
	cs, _ := nested(t, dep, "spec", "template", "spec", "containers").([]any)
	c := asMap(t, cs[0], "container")
	froms, _ := c["envFrom"].([]any)
	for _, f := range froms {
		fm := asMap(t, f, "envFrom[]")
		sr, ok := fm["secretRef"].(map[string]any)
		if !ok {
			continue
		}
		n, _ := sr["name"].(string)
		if n != "substrate-server" {
			t.Errorf("server envFrom secretRef %q: only the substrate-server Secret may be injected; the MinIO keys live in substrate-backups-s3 and are read by CNPG", n)
		}
	}
	// And the backup secret must exist for CNPG to reference.
	sec := docOfKind(t, "deploy/cnpg/backups-secret.yaml", "Secret")
	if str(t, nested(t, sec, "metadata", "name"), "name") != "substrate-backups-s3" {
		t.Error("backups secret must be named substrate-backups-s3")
	}
	cluster := string(readFile(t, "deploy/cnpg/cluster.yaml"))
	if !strings.Contains(cluster, "substrate-backups-s3") {
		t.Error("cluster.yaml s3Credentials must reference substrate-backups-s3")
	}
	// Nothing may still point at the old combined secret.
	for _, rel := range []string{"deploy/cnpg/cluster.yaml", "deploy/cnpg/scratch-cluster.yaml", "deploy/server/deployment.yaml"} {
		if strings.Contains(string(readFile(t, rel)), "name: substrate\n") {
			t.Errorf("%s still references the combined `substrate` Secret", rel)
		}
	}
}

// enableSuperuserAccess creates a postgres password that nothing at runtime
// needs, and a superuser password that exists is one that ends up in a DSN.
// CNPG defaults it off deliberately.
//
// Red when: `enableSuperuserAccess: true` is added back to cluster.yaml.
func TestProductionClusterHasNoSuperuserAccess(t *testing.T) {
	raw := string(readFile(t, "deploy/cnpg/cluster.yaml"))
	if strings.Contains(raw, "enableSuperuserAccess: true") {
		t.Fatal("enableSuperuserAccess must stay off (CNPG's default); nothing at runtime connects as postgres")
	}
	if strings.Contains(raw, "superuserSecret") {
		t.Fatal("no superuserSecret: there is no superuser password for the operator to hold")
	}
	// The runbook must name the role that actually goes in the DSN, or the
	// operator has nothing to put there and reaches for postgres.
	rb := string(readFile(t, "docs/ops/runbook.md"))
	if !strings.Contains(rb, "substrate-pg-app") {
		t.Fatal("runbook must name the CNPG-generated substrate-pg-app secret as the source of SUBSTRATE_DSN")
	}
	if !strings.Contains(rb, "Never `postgres`") {
		t.Fatal("runbook must say explicitly that SUBSTRATE_DSN is never the postgres superuser")
	}
}

// cert-manager cannot answer an HTTP-01 challenge for a non-public tailnet
// name, and Tailscale DNS is not a DNS-01 provider. Applying this Certificate
// leaves it Ready=False forever, the substrate-tls Secret never appears, and
// Traefik silently serves its self-signed default.
//
// Red when: ingress/certificate.yaml is added to kustomization resources, or
// the runbook stops naming a mechanism that actually works.
func TestTailnetTLSDoesNotRelyOnPublicACME(t *testing.T) {
	res := toStrings(docOfKind(t, "deploy/kustomization.yaml", "Kustomization")["resources"])
	for _, r := range res {
		if strings.Contains(r, "certificate.yaml") {
			t.Fatal("ingress/certificate.yaml must not be applied by default: cert-manager cannot issue for a *.ts.net name, and the Certificate would sit Ready=False while Traefik serves a self-signed default")
		}
	}
	cert := string(readFile(t, "deploy/ingress/certificate.yaml"))
	if strings.Contains(cert, "<cluster-issuer>") {
		t.Fatal("the bare <cluster-issuer> placeholder invites a public ACME issuer, which cannot work here")
	}
	rb := string(readFile(t, "docs/ops/runbook.md"))
	if !strings.Contains(rb, "tailscale cert") {
		t.Fatal("runbook must name the real mechanism (`tailscale cert` into the substrate-tls Secret, or an internal CA issuer)")
	}
	if !strings.Contains(rb, "substrate-tls") {
		t.Fatal("runbook must say which Secret the certificate has to land in")
	}
}

// deploy/alerts.yaml alerts on substrate_* series. If nothing receives the
// server's OTLP export and re-exposes it for scrape, those rules evaluate to
// no data and stay green forever — an alert that cannot fire.
//
// Red when: deploy/otel/collector.yaml is removed, or
// SUBSTRATE_OTLP_ENDPOINT in the ConfigMap is emptied.
func TestOTelCollectorExistsAndIsWired(t *testing.T) {
	svc := docOfKind(t, "deploy/otel/collector.yaml", "Service")
	svcName := str(t, nested(t, svc, "metadata", "name"), "svc.name")

	cm := docOfKind(t, "deploy/server/configmap.yaml", "ConfigMap")
	data := asMap(t, cm["data"], "data")
	ep, _ := data["SUBSTRATE_OTLP_ENDPOINT"].(string)
	if ep == "" {
		t.Fatal("SUBSTRATE_OTLP_ENDPOINT is empty: export is disabled, so every rule in deploy/alerts.yaml alerts on a series nothing produces")
	}
	if !strings.Contains(ep, svcName) {
		t.Fatalf("SUBSTRATE_OTLP_ENDPOINT %q does not point at the collector Service %q", ep, svcName)
	}

	// The receiver port in the endpoint must be one the Service exposes.
	ports, _ := nested(t, svc, "spec", "ports").([]any)
	matched := false
	for _, p := range ports {
		pm := asMap(t, p, "port")
		var n int
		switch v := pm["port"].(type) {
		case int:
			n = v
		case int64:
			n = int(v)
		}
		if n != 0 && strings.Contains(ep, ":"+itoa(n)) {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("SUBSTRATE_OTLP_ENDPOINT %q names a port the collector Service does not expose", ep)
	}

	res := toStrings(docOfKind(t, "deploy/kustomization.yaml", "Kustomization")["resources"])
	found := false
	for _, r := range res {
		if strings.Contains(r, "otel/collector.yaml") {
			found = true
		}
	}
	if !found {
		t.Error("otel/collector.yaml is not in kustomization resources")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Both restore scripts run inside a kubectl image. rancher/kubectl and
// alpine/k8s are busybox-only: a bash script with [[ ]], =~ and (( )) dies
// with a syntax error AFTER the counts were collected, and the operator then
// debugs the backups instead of the shell.
//
// Red when: restore-assert.sh goes back to `#!/usr/bin/env bash`, or gains a
// bashism.
func TestRestoreScriptsArePOSIXSh(t *testing.T) {
	for _, rel := range []string{"deploy/restore/restore-test.sh", "deploy/restore/restore-assert.sh"} {
		raw := string(readFile(t, rel))
		if !strings.HasPrefix(raw, "#!/bin/sh\n") {
			first, _, _ := strings.Cut(raw, "\n")
			t.Errorf("%s: shebang is %q, want #!/bin/sh — it is invoked from a /bin/sh script inside a busybox-only kubectl image", rel, first)
		}
		if strings.Contains(raw, "pipefail") {
			t.Errorf("%s: `set -o pipefail` is not POSIX and busybox ash rejects it", rel)
		}
		// Scan code only. Both scripts explain in comments which bashisms they
		// avoid and why, and a comment is not a parse error.
		var code strings.Builder
		for _, line := range strings.Split(raw, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			code.WriteString(line)
			code.WriteString("\n")
		}
		for _, bashism := range []string{"[[ ", "=~", "(( ", "local ", "declare ", "BASH_REMATCH", "${!"} {
			if strings.Contains(code.String(), bashism) {
				t.Errorf("%s: contains bashism %q; busybox sh fails at parse time, after the counts were already collected", rel, bashism)
			}
		}
	}

	// And prove it actually runs under a POSIX shell, not just that it looks
	// like one. dash is the reference POSIX sh on Debian/Ubuntu runners.
	sh, err := exec.LookPath("dash")
	if err != nil {
		sh, err = exec.LookPath("busybox")
		if err != nil {
			t.Fatal("neither dash nor busybox is installed, so the POSIX claim cannot be executed. Fix: apt-get install -y dash (or busybox). This is a failure, not a skip — an unexecuted POSIX claim is how the bash version shipped.")
		}
	}
	assert := filepath.Join(repoRoot(t), "deploy/restore/restore-assert.sh")
	args := []string{assert}
	if filepath.Base(sh) == "busybox" {
		args = append([]string{"sh"}, args...)
	}
	run := func(a ...string) (string, error) {
		cmd := exec.Command(sh, append(append([]string{}, args...), a...)...) //nolint:gosec // argv is test-controlled
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run("5", "5", "5"); err != nil {
		t.Errorf("POSIX shell rejected a passing case: %v\n%s", err, out)
	}
	// The behaviour under the POSIX shell must be the same one the bash
	// version had: zero rows is a failure.
	for _, c := range [][3]string{{"0", "5", "5"}, {"5", "0", "5"}, {"5", "5", "0"}} {
		out, err := run(c[0], c[1], c[2])
		if err == nil {
			t.Errorf("POSIX shell accepted a zero-row restore %v:\n%s", c, out)
		}
		if !strings.Contains(out, "0 rows") {
			t.Errorf("failure for %v did not name the zero-row cause:\n%s", c, out)
		}
	}
	if out, err := run("x", "5", "5"); err == nil {
		t.Errorf("POSIX shell accepted a non-integer count:\n%s", out)
	}
}

// The CronJob image must be digest-pinned and must be a placeholder in git.
// A tag would let the weekly job silently gain a different kubectl — or a
// different shell — between one Sunday and the next.
//
// Red when: cronjob.yaml goes back to a bare `<kubectl-image>` with no digest
// requirement, or the runbook stops requiring the digest form.
func TestRestoreImageMustBeDigestPinned(t *testing.T) {
	raw := string(readFile(t, "deploy/restore/cronjob.yaml"))
	if !strings.Contains(raw, "digest") {
		t.Fatal("cronjob.yaml must require a digest-pinned image; a mutable tag changes the shell under the weekly job")
	}
	rb := string(readFile(t, "docs/ops/runbook.md"))
	if !strings.Contains(rb, "@sha256:") {
		t.Fatal("runbook step 2 must require images pinned by digest")
	}
}

// The scanner that enforces the above.
func placeholderScanner(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "scripts", "check-deploy-placeholders.sh")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("scripts/check-deploy-placeholders.sh missing: %v", err)
	}
	return p
}

func runPlaceholders(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(placeholderScanner(t), args...) //nolint:gosec // argv is test-controlled
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The committed tree must still be fully un-substituted. This is the check
// that would have caught `imageName: substrate-pg:16-pgvector` and
// `image: ko.local/substrate-server:latest`, which read as already-substituted
// values and so were waved through by the secret scanner and every test here.
func TestCommittedDeployTreeIsFullyPlaceholdered(t *testing.T) {
	out, err := runPlaceholders(t, "--committed", "deploy")
	if err != nil {
		t.Fatalf("committed deploy/ is not fully placeholdered:\n%s", out)
	}
}

// A checker that has never been shown a bad tree proves nothing.
func TestPlaceholderScannerCatchesTagShapedImage(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "dep.yaml")
	body := "apiVersion: apps/v1\nkind: Deployment\nspec:\n  template:\n    spec:\n      containers:\n        - name: x\n          image: ko.local/substrate-server:latest\n"
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runPlaceholders(t, "--committed", dir)
	if err == nil {
		t.Fatalf("scanner accepted a tag-shaped image literal:\n%s", out)
	}
	if !strings.Contains(out, "ko.local/substrate-server:latest") {
		t.Fatalf("failure did not name the offending image:\n%s", out)
	}
}

func TestPlaceholderScannerCatchesUnsubstitutedAndUnpinned(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.yaml")
	body := "kind: Deployment\nimage: \"<some-image>\"\nhost: substrate.<tailnet>\n"
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runPlaceholders(t, "--substituted", dir)
	if err == nil {
		t.Fatalf("scanner accepted an unsubstituted tree in --substituted mode:\n%s", out)
	}
	if !strings.Contains(out, "<tailnet>") {
		t.Fatalf("failure did not name the surviving placeholder:\n%s", out)
	}

	// Substituted, but with a mutable tag rather than a digest.
	g := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(g, []byte("kind: Deployment\nimage: rancher/kubectl:v1.31.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = runPlaceholders(t, "--substituted", dir)
	if err == nil {
		t.Fatalf("scanner accepted a tag rather than a digest:\n%s", out)
	}
	if !strings.Contains(out, "not pinned by digest") {
		t.Fatalf("failure did not name the missing digest:\n%s", out)
	}
}

// `password: ""` applies as a literal empty password. Empty is the right
// committed state and the wrong applied state; the checker must tell them
// apart.
func TestPlaceholderScannerCatchesEmptySecretAfterSubstitution(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "secret.yaml")
	if err := os.WriteFile(f, []byte("kind: Secret\nstringData:\n  password: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runPlaceholders(t, "--committed", dir); err != nil {
		t.Fatalf("empty value must be allowed in the committed tree:\n%s", out)
	}
	out, err := runPlaceholders(t, "--substituted", dir)
	if err == nil {
		t.Fatalf("scanner accepted an empty password after substitution:\n%s", out)
	}
	if !strings.Contains(out, "still empty") {
		t.Fatalf("failure did not name the empty value:\n%s", out)
	}
}

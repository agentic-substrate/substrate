#!/bin/sh
# Weekly (and first-run) restore into substrate-pg-scratch, then assert that
# memory, instruction and audit each have at least one row. Zero is a failure.
#
# POSIX sh only. The kubectl images that can run this (rancher/kubectl,
# alpine/k8s) are busybox-only — no bash, no [[ ]], no (( )).
set -eu

NS=${NAMESPACE:-substrate}
SCRATCH=substrate-pg-scratch
MANIFEST=/restore/scratch-cluster.yaml
ASSERT=/restore/restore-assert.sh

# Clean up ONLY on success. The one week this job matters is the week it fails,
# and an unconditional `trap cleanup EXIT` deletes the scratch cluster —
# destroying the only evidence — before the operator can look at it. A failed
# run leaves the cluster behind and says so; the next run's pre-flight delete
# (below) reclaims the PVC.
cleanup() {
  kubectl delete cluster "$SCRATCH" -n "$NS" --ignore-not-found=true --wait=true
}

fail() {
  echo "restore-test: FAILED. Leaving cluster/${SCRATCH} in namespace ${NS} for" >&2
  echo "restore-test: inspection — it is NOT cleaned up on failure. Investigate," >&2
  echo "restore-test: then: kubectl delete cluster ${SCRATCH} -n ${NS}" >&2
  echo "restore-test: (the next run deletes it before restoring, so a forgotten" >&2
  echo "restore-test: scratch cluster costs one 20Gi Longhorn volume, not a rerun)" >&2
  exit 1
}
trap fail EXIT

kubectl delete cluster "$SCRATCH" -n "$NS" --ignore-not-found=true --wait=true
kubectl apply -f "$MANIFEST"
kubectl wait --for=condition=Ready "cluster/${SCRATCH}" -n "$NS" --timeout=1800s

pod="${SCRATCH}-1"
kubectl wait --for=condition=Ready "pod/${pod}" -n "$NS" --timeout=600s

count() {
  table=$1
  kubectl exec -n "$NS" "$pod" -- psql -U postgres -d substrate -tAqc "SELECT count(*) FROM ${table}"
}

memory=$(count memory)
instruction=$(count instruction)
audit=$(count audit)

"$ASSERT" "$memory" "$instruction" "$audit"

# Only now is it safe to reclaim the volume.
trap - EXIT
cleanup

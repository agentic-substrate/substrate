#!/bin/sh
# Weekly (and first-run) restore into substrate-pg-scratch, then assert that
# memory, instruction and audit each have at least one row. Zero is a failure.
set -eu

NS=${NAMESPACE:-substrate}
SCRATCH=substrate-pg-scratch
MANIFEST=/restore/scratch-cluster.yaml
ASSERT=/restore/restore-assert.sh

cleanup() {
  kubectl delete cluster "$SCRATCH" -n "$NS" --ignore-not-found=true --wait=true
}
trap cleanup EXIT

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

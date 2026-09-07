#!/usr/bin/env bash
# Operator-run MinIO setup. Do not execute from CI or an unattended worker.
# Substitute every <placeholder> on the operator machine. Never commit filled values.
#
# Blast radius: creates two buckets on the existing MinIO. A name collision with
# an in-use bucket is the failure mode — check `mc ls` first.
set -euo pipefail

cat <<'EOF'
# 1. Alias (credentials stay on the operator machine)
mc alias set substrate-minio 'http://<minio-endpoint>' '<access-key>' '<secret-key>'

# 2. Inspect existing buckets before creating anything
mc ls substrate-minio

# 3. Backup bucket: continuous WAL + daily base backups (CNPG barman-cloud)
mc mb --ignore-existing substrate-minio/substrate-backups
mc encrypt set sse-s3 substrate-minio/substrate-backups
mc anonymous set none substrate-minio/substrate-backups

# 4. Session bucket: reserved for Phase 3, owner-only
mc mb --ignore-existing substrate-minio/substrate-sessions
mc encrypt set sse-s3 substrate-minio/substrate-sessions
mc anonymous set none substrate-minio/substrate-sessions
mc admin policy create substrate-minio substrate-sessions-owner deploy/minio/session-policy.json
mc admin policy attach substrate-minio substrate-sessions-owner --user '<bucket-owner>'
EOF

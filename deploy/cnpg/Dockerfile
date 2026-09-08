# CloudNativePG Postgres 16 plus pgvector. pg_trgm and ltree ship in contrib.
# Build and push this image from the operator machine; do not apply the Cluster
# until imageName in cluster.yaml points at the digest you pushed.
#
# The base is a <placeholder> for the same reason every `image:` in deploy/ is.
# A mutable tag here means the Postgres binary under a 20Gi Longhorn volume can
# change between one `docker build` and the next without anything saying so,
# and this is the one image in the repo we actually build. Substitute with
# ghcr.io/cloudnative-pg/postgresql@sha256:… — resolve the digest with
# `docker buildx imagetools inspect ghcr.io/cloudnative-pg/postgresql:16-standard-trixie`.
FROM <cnpg-base-image-digest>
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends postgresql-16-pgvector \
    && rm -rf /var/lib/apt/lists/*
USER 26

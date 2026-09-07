# CloudNativePG Postgres 16 plus pgvector. pg_trgm and ltree ship in contrib.
# Build and load this image on the operator machine; do not apply the Cluster
# until imageName in cluster.yaml points at what you built.
FROM ghcr.io/cloudnative-pg/postgresql:16-standard-trixie
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends postgresql-16-pgvector \
    && rm -rf /var/lib/apt/lists/*
USER 26

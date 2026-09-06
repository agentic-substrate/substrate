# Test/smoke Postgres 16 with pgvector. pg_trgm and ltree ship in contrib.
# Built from the locally-cached postgres:16-alpine when Docker Hub cannot
# serve pgvector/pgvector:pg16 (the preferred image in CI).
FROM postgres:16-alpine

RUN apk add --no-cache --virtual .build-deps \
        git build-base clang llvm postgresql16-dev \
    && git clone --branch v0.8.1 --depth 1 https://github.com/pgvector/pgvector.git /tmp/pgvector \
    && make -C /tmp/pgvector OPTFLAGS="" with_llvm=no \
    && make -C /tmp/pgvector install with_llvm=no \
    && apk del .build-deps \
    && rm -rf /tmp/pgvector /root/.cache

FROM --platform=$BUILDPLATFORM golang:1.26.4-bookworm@sha256:b305420a68d0f229d91eb3b3ed9e519fcf2cf5461da4bef997bf927e8c0bfd2b AS backend
WORKDIR /build
COPY repos/happyro-api/go.mod repos/happyro-api/go.sum ./
RUN go mod download
COPY repos/happyro-api/cmd/ ./cmd/
COPY repos/happyro-api/internal/ ./internal/
COPY repos/happyro-api/migrations/ ./migrations/
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/happyro-api ./cmd/happyro-api

FROM debian:bookworm-slim AS payload
COPY repos/happyro-api/resources/game-data/ /payload/opt/api/resources/game-data/
COPY repos/happyro-server/db/map_index.txt /payload/opt/api/resources/map_index.txt
COPY repos/happyro-api/resources/game-data/items/renewal.json /payload/opt/happyro/catalog-base/items/renewal.json
COPY repos/happyro-api/resources/game-data/monsters/renewal.json /payload/opt/happyro/catalog-base/monsters/renewal.json
COPY repos/happyro-api/resources/game-data/world/npc-catalog.json /payload/opt/happyro/catalog-base/world/npc-catalog.json
COPY repos/happyro-server/npc/ /payload/opt/happyro/server-base/npc/
COPY repos/happyro-server/conf/ /payload/opt/happyro/server-base/conf/
COPY repos/happyro-server/conf/import-tmpl/ /payload/opt/happyro/server-base/conf/import/
COPY repos/happyro-api/tools/catalogs.py /payload/opt/happyro/catalogs.py
COPY repos/happyro-api/deploy/docker/entrypoint.sh /payload/usr/local/bin/happyro-entrypoint
RUN chown -R 33:33 /payload/opt/api/resources/game-data \
    && find /payload -type d -exec chmod 755 {} + \
    && find /payload -type f -exec chmod a+r {} + \
    && chmod 755 /payload/usr/local/bin/happyro-entrypoint

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl gosu python3 python3-yaml \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/api
COPY --from=backend --chmod=755 /out/happyro-api /usr/local/bin/happyro-api
COPY --from=payload /payload/ /
ENV GIN_MODE=release HTTP_LISTEN=:8080 LOG_LEVEL=info
EXPOSE 8080
ENTRYPOINT ["happyro-entrypoint"]
CMD ["serve"]

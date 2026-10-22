FROM --platform=$BUILDPLATFORM node:22-bookworm AS frontend
WORKDIR /build
COPY repos/happyro-admin/package*.json ./
COPY repos/happyro-admin/.npmrc ./
RUN npm ci --ignore-scripts
COPY repos/happyro-admin/ ./
RUN npx max setup && npm run build

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

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl gosu python3 python3-yaml \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/api
COPY --from=backend /out/happyro-api /usr/local/bin/happyro-api
COPY --from=frontend /build/dist/ /opt/admin-ui/
COPY repos/happyro-api/resources/game-data/ /opt/api/resources/game-data/
COPY repos/happyro-server/db/map_index.txt /opt/api/resources/map_index.txt
COPY repos/happyro-api/resources/game-data/items/renewal.json /opt/happyro/catalog-base/items/renewal.json
COPY repos/happyro-api/resources/game-data/monsters/renewal.json /opt/happyro/catalog-base/monsters/renewal.json
COPY repos/happyro-api/resources/game-data/world/npc-catalog.json /opt/happyro/catalog-base/world/npc-catalog.json
COPY repos/happyro-server/npc/ /opt/happyro/server-base/npc/
COPY repos/happyro-server/conf/ /opt/happyro/server-base/conf/
COPY repos/happyro-server/conf/import-tmpl/ /opt/happyro/server-base/conf/import/
COPY tools/deployment/catalogs.py /opt/happyro/catalogs.py
COPY repos/happyro-api/deploy/docker/entrypoint.sh /usr/local/bin/happyro-entrypoint
RUN chmod 755 /usr/local/bin/happyro-entrypoint /usr/local/bin/happyro-api \
    && chown -R www-data:www-data /opt/api/resources/game-data \
    && find /opt/api /opt/admin-ui -type d -exec chmod 755 {} + \
    && find /opt/api /opt/admin-ui -type f -exec chmod a+r {} +
ENV GIN_MODE=release HTTP_LISTEN=:8080 FRONTEND_DIR=/opt/admin-ui LOG_LEVEL=info
EXPOSE 8080
ENTRYPOINT ["happyro-entrypoint"]
CMD ["serve"]

FROM node:20-bookworm AS frontend
ARG NPM_CONFIG_REGISTRY=https://registry.npmjs.org/
ENV NPM_CONFIG_REGISTRY=${NPM_CONFIG_REGISTRY}
WORKDIR /src/frontend
COPY frontend/package.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-bookworm AS backend
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src/backend
COPY backend/go.mod ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/lpicto ./cmd/server

FROM debian:bookworm-slim AS runtime
ARG APT_MIRROR=http://deb.debian.org/debian
ARG APT_SECURITY_MIRROR=http://deb.debian.org/debian-security
ARG APT_HTTP_PROXY=
ARG APT_HTTPS_PROXY=
RUN set -eux; \
  if [ -n "${APT_HTTP_PROXY}" ]; then echo "Acquire::http::Proxy \"${APT_HTTP_PROXY}\";" > /etc/apt/apt.conf.d/01proxy; fi; \
  if [ -n "${APT_HTTPS_PROXY}" ]; then echo "Acquire::https::Proxy \"${APT_HTTPS_PROXY}\";" >> /etc/apt/apt.conf.d/01proxy; fi; \
  sed -i \
    -e "s|http://deb.debian.org/debian-security|${APT_SECURITY_MIRROR}|g" \
    -e "s|http://deb.debian.org/debian|${APT_MIRROR}|g" \
    /etc/apt/sources.list.d/debian.sources \
  && apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    libimage-exiftool-perl \
    libvips-tools \
    tzdata \
  && rm -rf /var/lib/apt/lists/*

RUN useradd --system --uid 10001 --create-home --home-dir /nonexistent --shell /usr/sbin/nologin lpicto \
  && mkdir -p /app/frontend/dist /app/migrations /photos /data/cache/thumbs /data/cache/previews /data/cache/video-posters /data/cache/video-proxies \
  && chown -R lpicto:lpicto /app /data

WORKDIR /app
COPY --from=backend --chown=lpicto:lpicto /out/lpicto /app/lpicto
COPY --from=frontend --chown=lpicto:lpicto /src/frontend/dist /app/frontend/dist
COPY --chown=lpicto:lpicto backend/migrations /app/migrations
RUN chmod -R a+rX /app

ENV PHOTO_ROOT=/photos \
    DATA_ROOT=/data \
    HTTP_ADDR=:8080 \
    STATIC_DIR=/app/frontend/dist \
    MIGRATIONS_DIR=/app/migrations

USER lpicto
EXPOSE 8080
ENTRYPOINT ["/app/lpicto"]

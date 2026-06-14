FROM node:20-bookworm AS frontend
WORKDIR /src/frontend
COPY frontend/package.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-bookworm AS backend
WORKDIR /src/backend
COPY backend/go.mod ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/lpicto ./cmd/server

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
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
COPY --from=backend /out/lpicto /app/lpicto
COPY --from=frontend /src/frontend/dist /app/frontend/dist
COPY backend/migrations /app/migrations

ENV PHOTO_ROOT=/photos \
    DATA_ROOT=/data \
    HTTP_ADDR=:8080 \
    STATIC_DIR=/app/frontend/dist \
    MIGRATIONS_DIR=/app/migrations

USER lpicto
EXPOSE 8080
ENTRYPOINT ["/app/lpicto"]

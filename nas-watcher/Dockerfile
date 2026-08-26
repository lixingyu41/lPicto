FROM golang:1.25-bookworm AS build
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go main_test.go ./
RUN go test ./... \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/lpicto-nas-watcher .

FROM scratch
COPY --from=build /out/lpicto-nas-watcher /usr/local/bin/lpicto-nas-watcher
ENTRYPOINT ["/usr/local/bin/lpicto-nas-watcher"]

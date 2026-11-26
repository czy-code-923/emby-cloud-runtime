FROM golang:1.26.8-bookworm AS build
WORKDIR /src
COPY upstream/twilight/go.mod upstream/twilight/go.sum ./
RUN go mod download && go mod verify
COPY upstream/twilight/ ./
COPY VERSION /build-version
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X github.com/prejudice-studio/twilight/internal/config.BuildVersion=$(cat /build-version)" -o /out/twilight ./cmd/twilight
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata curl && rm -rf /var/lib/apt/lists/* && useradd -u 10001 -m twilight
COPY --from=build /out/twilight /usr/local/bin/twilight
USER 10001:10001
WORKDIR /var/lib/twilight
ENTRYPOINT ["/usr/local/bin/twilight"]
CMD ["all","--host","0.0.0.0","--port","5000","--config","config.toml"]

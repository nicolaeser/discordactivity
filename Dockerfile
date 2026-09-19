# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.25

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-trixie AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/discordactivity ./cmd/discordactivity

FROM debian:trixie-slim AS runtime
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --gid 65532 nonroot \
 && useradd --uid 65532 --gid 65532 --no-create-home --shell /usr/sbin/nologin nonroot \
 && mkdir -p /var/lib/discordactivity \
 && chown 65532:65532 /var/lib/discordactivity
COPY --from=build /out/discordactivity /usr/local/bin/discordactivity
USER nonroot:nonroot
EXPOSE 8080
ENV PORT=8080
ENV SQLITE_PATH=/var/lib/discordactivity/data.sqlite
ENV GOMAXPROCS=2
LABEL org.opencontainers.image.title="DiscordActivity"
LABEL org.opencontainers.image.description="Host Discord presence and activities for one or more user tokens."
LABEL org.opencontainers.image.source="https://github.com/nicolaeser/discordactivity"
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD ["/usr/local/bin/discordactivity", "-healthcheck"]
ENTRYPOINT ["/usr/local/bin/discordactivity"]

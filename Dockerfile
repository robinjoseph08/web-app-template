# syntax=docker/dockerfile:1.7

# The build stages run on the build machine's own platform and cross-compile
# for the target, so a multi-platform release doesn't build under emulation.

# The pnpm and tygo versions pinned in mise.toml, so local and image builds
# match. They're read in their own stage so other edits to mise.toml don't
# redo the dependency installs below.
FROM --platform=$BUILDPLATFORM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS tool-versions
COPY mise.toml /
RUN sed -n 's/^"npm:pnpm" = "\(.*\)"$/\1/p' /mise.toml > /pnpm-version && \
    sed -n 's/^"github:gzuidhof\/tygo" = "\(.*\)"$/\1/p' /mise.toml > /tygo-version && \
    test -s /pnpm-version && test -s /tygo-version

FROM --platform=$BUILDPLATFORM golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS typegen
WORKDIR /src
COPY --from=tool-versions /tygo-version /tmp/tygo-version
RUN go install "github.com/gzuidhof/tygo@v$(cat /tmp/tygo-version)"
COPY go.mod go.sum tygo.yaml ./
COPY pkg ./pkg
RUN tygo generate

FROM --platform=$BUILDPLATFORM node:26.8.1-alpine@sha256:2d984a15c9b54fd0aeb608b8e0d0d83529eb34d2966db27a1fb4f1edc3d298a3 AS frontend
WORKDIR /src
COPY --from=tool-versions /pnpm-version /tmp/pnpm-version
RUN npm install --global "pnpm@$(cat /tmp/pnpm-version)"
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY app ./app
COPY tsconfig*.json vite.config.ts ./
COPY --from=typegen /src/app/types/generated ./app/types/generated
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS backend
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY --from=frontend /src/internal/webapp/dist ./internal/webapp/dist
RUN MODULE=$(go list -m) && \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -trimpath -installsuffix cgo \
      -ldflags "-w -s -X $MODULE/pkg/version.Version=$VERSION" \
      -o /out/app ./cmd/api

FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app && \
    mkdir -p /config && chown -R app:app /config
COPY --from=backend --chown=app:app /out/app /usr/local/bin/app
USER app
ENV SERVER_HOST=0.0.0.0
EXPOSE 3579
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD wget --quiet --output-document=/dev/null http://127.0.0.1:3579/health || exit 1
ENTRYPOINT ["/usr/local/bin/app"]

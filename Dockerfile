# syntax=docker/dockerfile:1.7

# The build stages run on the build machine's own platform and cross-compile
# for the target, so a multi-platform release doesn't build under emulation.

# The pnpm and tygo versions pinned in mise.toml, so local and image builds
# match. They're read in their own stage so other edits to mise.toml don't
# redo the dependency installs below.
FROM --platform=$BUILDPLATFORM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS tool-versions
COPY mise.toml /
RUN sed -n 's/^"npm:pnpm" = "\(.*\)"$/\1/p' /mise.toml > /pnpm-version && \
    sed -n 's/^"github:gzuidhof\/tygo" = "\(.*\)"$/\1/p' /mise.toml > /tygo-version && \
    test -s /pnpm-version && test -s /tygo-version

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS typegen
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

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS backend
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

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
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

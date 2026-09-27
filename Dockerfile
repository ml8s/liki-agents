# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY contracts ./contracts
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w -X github.com/ml8s/liki-agents/internal/platform/buildinfo.Version=${VERSION} -X github.com/ml8s/liki-agents/internal/platform/buildinfo.Commit=${COMMIT} -X github.com/ml8s/liki-agents/internal/platform/buildinfo.BuildTime=${BUILD_TIME}" -o /out/liki-agents ./cmd/liki-agents
RUN mkdir -p /data && chown 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
COPY --from=build /out/liki-agents /liki-agents
COPY --from=build --chown=nonroot:nonroot /data /data
LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}" \
      org.opencontainers.image.title="liki-agents" \
      org.opencontainers.image.description="Domain-neutral Liki multi-agent runtime"
ENV LIKI_ENV=production LIKI_AGENTS_ADDR=:8083 LIKI_AGENTS_DATA_DIR=/data LIKI_LOG_FORMAT=json
ENV LIKI_AGENTS_PUBLIC_URL=http://localhost:8083
VOLUME ["/data"]
EXPOSE 8083
USER nonroot:nonroot
ENTRYPOINT ["/liki-agents"]

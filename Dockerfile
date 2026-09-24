FROM golang:1.26.6-alpine AS build
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY contracts ./contracts
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w -X github.com/ml8s/liki-agents/internal/platform/buildinfo.Version=${VERSION} -X github.com/ml8s/liki-agents/internal/platform/buildinfo.Commit=${COMMIT} -X github.com/ml8s/liki-agents/internal/platform/buildinfo.BuildTime=${BUILD_TIME}" -o /out/liki-agents ./cmd/liki-agents
RUN mkdir -p /data && chown 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/liki-agents /liki-agents
COPY --from=build --chown=nonroot:nonroot /data /data
ENV LIKI_ENV=production LIKI_AGENTS_ADDR=:8083 LIKI_AGENTS_DATA_DIR=/data LIKI_LOG_FORMAT=json
ENV LIKI_AGENTS_PUBLIC_URL=http://localhost:8083
VOLUME ["/data"]
EXPOSE 8083
USER nonroot:nonroot
ENTRYPOINT ["/liki-agents"]

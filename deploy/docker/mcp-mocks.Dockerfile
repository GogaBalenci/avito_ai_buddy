# mcp-mocks — имитации корпоративных сервисов (MCP). Наружу порт не публикуется.
# Все базовые образы multi-arch (amd64 + arm64). REGISTRY_PREFIX — см. api.Dockerfile.
ARG REGISTRY_PREFIX=docker.io/
ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.22

FROM ${REGISTRY_PREFIX}library/golang:${GO_VERSION}-alpine AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV CGO_ENABLED=0 GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/mcp-mocks ./cmd/mcp-mocks

FROM ${REGISTRY_PREFIX}library/alpine:${ALPINE_VERSION}
COPY --from=build /out/mcp-mocks /usr/local/bin/mcp-mocks
USER 65532:65532
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/mcp-mocks"]

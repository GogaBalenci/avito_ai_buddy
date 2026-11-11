# api — HTTP API AI-buddy. Многостадийная сборка, статический бинарник в alpine.
# Все базовые образы multi-arch (amd64 + arm64). REGISTRY_PREFIX позволяет
# тянуть их через зеркало (mirror.gcr.io/), если Docker Hub недоступен.
# Строки "# syntax=" нет намеренно: она тянет frontend с Docker Hub в обход REGISTRY_PREFIX.
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
    go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM ${REGISTRY_PREFIX}library/alpine:${ALPINE_VERSION}
COPY --from=build /out/api /usr/local/bin/api
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]

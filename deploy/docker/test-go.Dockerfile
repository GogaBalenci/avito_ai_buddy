# test-go — тесты и покрытие Go в том же окружении, что в CI.
# Debian, а не alpine: для -race нужен cgo и glibc (12 §4.1).
ARG REGISTRY_PREFIX=docker.io/
ARG GO_VERSION=1.26

FROM ${REGISTRY_PREFIX}library/golang:${GO_VERSION}-bookworm
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY} OUT=/out
WORKDIR /src
COPY go.mod go.sum* ./
# Без cache-mount: модули должны остаться в образе, тесты запускаются позже через `run`
RUN go mod download
COPY . .
CMD ["sh", "scripts/test-go.sh"]

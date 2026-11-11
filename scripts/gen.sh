#!/bin/sh
# Generates Go code from api/openapi.yaml (12 §6). The generator is not a module
# dependency: `go run pkg@version` keeps it out of go.mod and the Docker build.
set -eu
OAPI_CODEGEN_VERSION=v2.8.2
mkdir -p internal/gen/api
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@"$OAPI_CODEGEN_VERSION" \
  -config api/oapi-codegen.yaml api/openapi.yaml
gofmt -w internal/gen/api

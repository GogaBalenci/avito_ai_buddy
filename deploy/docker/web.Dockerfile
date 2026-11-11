# web — SPA (Vite + React + TypeScript) за nginx; nginx проксирует /api и /healthz в api.
# Все базовые образы multi-arch (amd64 + arm64). REGISTRY_PREFIX — см. api.Dockerfile.
ARG REGISTRY_PREFIX=docker.io/
ARG NODE_VERSION=24
ARG NGINX_VERSION=1.28

FROM ${REGISTRY_PREFIX}library/node:${NODE_VERSION}-alpine AS build
ARG NPM_REGISTRY=https://registry.npmjs.org/
WORKDIR /src
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --registry="${NPM_REGISTRY}" --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM ${REGISTRY_PREFIX}library/nginx:${NGINX_VERSION}-alpine
COPY web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80

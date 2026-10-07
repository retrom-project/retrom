# syntax=docker/dockerfile:1.7
FROM golang:1.26.5-bookworm@sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd AS build
ENV GOMAXPROCS=2
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY api api
COPY cmd cmd
COPY internal internal
COPY migrations migrations
COPY scripts/openapi-bundle scripts/openapi-bundle
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p internal/httpapi/generated .cache/generated \
    && go run ./scripts/openapi-bundle -input api/openapi.yaml -output .cache/generated/openapi.bundle.yaml \
    && for config in api/codegen/*.yaml; do \
      go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 --config "$config" .cache/generated/openapi.bundle.yaml; \
    done \
    && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/retrom ./cmd/retrom

# Explicit BuildKit contexts runtime-tool, providers and runtime-dependencies are mandatory.
FROM node:24.18.0-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d
ARG IMAGE_INPUT_DIGEST
LABEL io.retrom.image-input-sha256=$IMAGE_INPUT_DIGEST
RUN mkdir -p /var/lib/retrom && chown 1000:1000 /var/lib/retrom
COPY --from=build --chmod=0555 /out/retrom /usr/local/bin/retrom
COPY --from=runtime-tool / /opt/retrom/runtime-tool/
COPY --from=providers /active.json /opt/retrom/providers/active.json
COPY --from=providers /installed/ /opt/retrom/providers/installed/
COPY --from=runtime-dependencies /auth/password-blocklists/ /opt/retrom/dependencies/auth/password-blocklists/
RUN chmod -R a=rX /opt/retrom/dependencies
COPY web/public/runtime-isolation/ /opt/retrom/web/public/runtime-isolation/
ENV RETROM_MODE=release \
    RETROM_HTTP_ADDR=0.0.0.0:8080 \
    RETROM_PUBLIC_ORIGIN=https://retrom.invalid \
    RETROM_DATA_DIR=/var/lib/retrom \
    RETROM_DEPENDENCY_ROOT=/opt/retrom/dependencies \
    RETROM_RUNTIME_ROOT=/opt/retrom/runtime-tool \
    RETROM_PROVIDER_ROOT=/opt/retrom/providers \
    RETROM_WEB_ROOT=/opt/retrom/web \
    RETROM_NODE=/usr/local/bin/node
USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/retrom"]

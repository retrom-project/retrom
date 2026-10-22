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

# Explicit BuildKit contexts runtime-tool and runtime-dependencies are mandatory. Providers are not in
# the image: the backend installs the pinned chunks into the data volume at startup.
FROM node:24.22.1-bookworm-slim@sha256:55fb5f5a527ea2c5c4a95f963e48ef90aa38c7b346054fdc8ee9957a2b55d205
ARG IMAGE_INPUT_DIGEST
LABEL io.retrom.image-input-sha256=$IMAGE_INPUT_DIGEST
RUN mkdir -p /var/lib/retrom && chown 1000:1000 /var/lib/retrom
COPY --from=build --chmod=0555 /out/retrom /usr/local/bin/retrom
# The Go backend downloads Provider chunks over HTTPS and trusts only the system roots; the slim
# Node base has none (Node bundles its own), so take them from the pinned build stage.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=runtime-tool / /opt/retrom/runtime-tool/
COPY --chmod=0444 data/runtime-inputs.json /opt/retrom/runtime-inputs.json
COPY --from=runtime-dependencies /auth/password-blocklists/ /opt/retrom/dependencies/auth/password-blocklists/
RUN chmod -R a=rX /opt/retrom/dependencies
COPY web/public/runtime-isolation/ /opt/retrom/web/public/runtime-isolation/
ENV RETROM_MODE=release \
    RETROM_HTTP_ADDR=0.0.0.0:8080 \
    RETROM_PUBLIC_ORIGIN=https://retrom.invalid \
    RETROM_DATA_DIR=/var/lib/retrom \
    RETROM_DEPENDENCY_ROOT=/opt/retrom/dependencies \
    RETROM_RUNTIME_ROOT=/opt/retrom/runtime-tool \
    RETROM_PROVIDER_INPUTS=/opt/retrom/runtime-inputs.json \
    RETROM_PROVIDER_ROOT=/var/lib/retrom/providers \
    RETROM_WEB_ROOT=/opt/retrom/web \
    RETROM_NODE=/usr/local/bin/node
USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/retrom"]

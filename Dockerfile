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

# The runtime host module is compiled into the backend; the image has no Node or host tool. The explicit
# runtime-dependencies context is mandatory. Providers are not in the image: the backend installs the pinned
# chunks into the data volume at startup.
FROM build AS layout
COPY data/runtime-inputs.json /layout/opt/retrom/runtime-inputs.json
COPY --from=runtime-dependencies /auth/password-blocklists/ /layout/opt/retrom/dependencies/auth/password-blocklists/
COPY web/public/runtime-isolation/ /layout/opt/retrom/web/public/runtime-isolation/
RUN chmod -R a=rX /layout/opt/retrom && mkdir -p /layout/var/lib/retrom

# Static base: CA roots for Provider chunk downloads and /tmp for the Provider's static ScummVM detector.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG IMAGE_INPUT_DIGEST
LABEL io.retrom.image-input-sha256=$IMAGE_INPUT_DIGEST
COPY --from=layout /layout/opt/retrom/ /opt/retrom/
COPY --from=layout --chown=1000:1000 /layout/var/lib/retrom/ /var/lib/retrom/
COPY --from=build --chmod=0555 /out/retrom /usr/local/bin/retrom
ENV RETROM_MODE=release \
    RETROM_HTTP_ADDR=0.0.0.0:8080 \
    RETROM_PUBLIC_ORIGIN=https://retrom.invalid \
    RETROM_DATA_DIR=/var/lib/retrom \
    RETROM_DEPENDENCY_ROOT=/opt/retrom/dependencies \
    RETROM_PROVIDER_INPUTS=/opt/retrom/runtime-inputs.json \
    RETROM_PROVIDER_ROOT=/var/lib/retrom/providers \
    RETROM_WEB_ROOT=/opt/retrom/web
USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/retrom"]

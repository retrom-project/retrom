# syntax=docker/dockerfile:1.7
FROM python:3.13-alpine AS dependencies
ARG RETROM_DEPENDENCY_VERSIONS=4.2.3,4.3.0-pre
WORKDIR /work
RUN apk add --no-cache build-base p7zip xz
COPY scripts/dependencies.py scripts/dependencies.py
COPY scripts/fbalpha2012_dat.py scripts/fbalpha2012_dat.py
COPY scripts/fbalpha2012-dat-enumerator.cpp scripts/fbalpha2012-dat-enumerator.cpp
COPY data/dat data/dat
COPY data/auth data/auth
COPY data/netplay data/netplay
COPY web/features/player/adapters web/features/player/adapters
COPY web/features/player/netplay web/features/player/netplay
RUN python3 scripts/dependencies.py prepare --versions "$RETROM_DEPENDENCY_VERSIONS" \
  && python3 scripts/dependencies.py image-export \
    --versions "$RETROM_DEPENDENCY_VERSIONS" \
    --output /work/image-dependencies \
  && rm -rf /work/data/runtime /work/data/dat /work/data/auth /work/data/netplay

FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api api
COPY cmd cmd
COPY internal internal
COPY migrations migrations
RUN mkdir -p internal/httpapi/generated \
  && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 --config api/oapi-codegen.yaml api/openapi.yaml \
  && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/retrom ./cmd/retrom

FROM alpine:3.22
ARG RELEASE_INPUT_DIGEST
LABEL io.retrom.release-input-sha256=$RELEASE_INPUT_DIGEST
RUN mkdir -p /var/lib/retrom
COPY --from=build --chmod=0555 /out/retrom /usr/local/bin/retrom
COPY --from=dependencies /work/image-dependencies /opt/retrom/dependencies
ENV RETROM_HTTP_ADDR=0.0.0.0:8080 \
    RETROM_PUBLIC_ORIGIN=https://retrom.invalid \
    RETROM_DATA_DIR=/var/lib/retrom \
    RETROM_DEPENDENCY_ROOT=/opt/retrom/dependencies \
    RETROM_DEPENDENCY_VERSIONS=4.2.3,4.3.0-pre \
    RETROM_ACTIVE_EMULATORJS_VERSION=4.2.3
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/retrom"]

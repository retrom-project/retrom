# syntax=docker/dockerfile:1.7
FROM python:3.13-alpine AS dependencies
ARG RETROM_DEPENDENCY_VERSIONS=4.2.3,4.3.0-pre
WORKDIR /work
RUN apk add --no-cache p7zip xz
COPY scripts/dependencies.py scripts/dependencies.py
COPY data/dat data/dat
COPY web/features/player/adapters web/features/player/adapters
RUN python3 scripts/dependencies.py prepare --versions "$RETROM_DEPENDENCY_VERSIONS" \
  && find data/runtime/emulatorjs -type f -name '*.7z' -delete \
  && find data/runtime/emulatorjs -type f -path '*/.downloads/*' -delete \
  && find data/runtime/emulatorjs -depth -type d -empty -delete

FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api api
COPY cmd cmd
COPY internal internal
COPY migrations migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/retrom ./cmd/retrom

FROM alpine:3.22
ARG RELEASE_INPUT_DIGEST
LABEL io.retrom.release-input-sha256=$RELEASE_INPUT_DIGEST
RUN addgroup -S -g 10001 retrom && adduser -S -D -H -u 10001 -G retrom retrom \
  && mkdir -p /var/lib/retrom /opt/retrom/dependencies \
  && chown -R retrom:retrom /var/lib/retrom
COPY --from=build /out/retrom /usr/local/bin/retrom
COPY --from=dependencies /work/data/dat/emulatorjs /opt/retrom/dependencies/dat/emulatorjs
COPY --from=dependencies /work/data/runtime/emulatorjs /opt/retrom/dependencies/runtime/emulatorjs
USER 10001:10001
ENV RETROM_HTTP_ADDR=0.0.0.0:8080 \
    RETROM_PUBLIC_ORIGIN=https://retrom.invalid \
    RETROM_DATA_DIR=/var/lib/retrom \
    RETROM_DEPENDENCY_ROOT=/opt/retrom/dependencies \
    RETROM_DEPENDENCY_VERSIONS=4.2.3,4.3.0-pre \
    RETROM_ACTIVE_EMULATORJS_VERSION=4.2.3
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/retrom"]

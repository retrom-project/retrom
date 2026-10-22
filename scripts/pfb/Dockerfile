FROM node:24.22.1-bookworm-slim@sha256:55fb5f5a527ea2c5c4a95f963e48ef90aa38c7b346054fdc8ee9957a2b55d205 AS node

FROM golang:1.26.5-bookworm@sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd
ARG PFB_TOOLCHAIN_DIGEST
RUN apt-get update \
    && apt-get install -y --no-install-recommends bash ca-certificates curl g++ git make p7zip-full python3 xz-utils \
    && rm -rf /var/lib/apt/lists/*
COPY --from=node /usr/local/ /usr/local/
RUN groupadd --gid 1000 pfb && useradd --uid 1000 --gid 1000 --create-home pfb
LABEL io.retrom.pfb-toolchain-sha256="${PFB_TOOLCHAIN_DIGEST}"
USER pfb
WORKDIR /workspace/retrom
ENTRYPOINT ["/workspace/retrom/scripts/pfb/entrypoint.sh"]

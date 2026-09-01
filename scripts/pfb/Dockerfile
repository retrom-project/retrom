FROM node:24.18.0-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d AS node

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

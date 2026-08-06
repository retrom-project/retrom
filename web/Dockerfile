# syntax=docker/dockerfile:1.7
FROM node:24.18.0-alpine AS dependencies
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:24.18.0-alpine AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=dependencies /app/node_modules node_modules
COPY . .
RUN npm run build

FROM node:24.18.0-alpine
ARG RELEASE_INPUT_DIGEST
LABEL io.retrom.release-input-sha256=$RELEASE_INPUT_DIGEST
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=3000
RUN addgroup -S -g 10001 retrom && adduser -S -D -H -u 10001 -G retrom retrom
COPY --from=build --chown=retrom:retrom /app/.next/standalone ./
COPY --from=build --chown=retrom:retrom /app/.next/static ./.next/static
USER 10001:10001
EXPOSE 3000
CMD ["node", "server.js"]

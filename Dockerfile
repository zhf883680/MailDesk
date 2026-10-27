# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:20-alpine AS web-build
WORKDIR /app/source/web
COPY source/web/package.json ./
RUN npm install --no-audit --no-fund
COPY source/web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS server-build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app/source
COPY source/go.mod source/go.sum ./
RUN go mod download
COPY source/ ./
COPY --from=web-build /app/source/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/maildesk .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 maildesk \
    && adduser -S -D -H -u 10001 -G maildesk maildesk \
    && mkdir -p /data \
    && chown -R maildesk:maildesk /data

ENV TZ=Asia/Shanghai

COPY --from=server-build /out/maildesk /usr/local/bin/maildesk

WORKDIR /app
USER maildesk
VOLUME ["/data"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:8080/ || exit 1

ENTRYPOINT ["maildesk"]
CMD ["-addr", ":8080", "-data", "/data"]

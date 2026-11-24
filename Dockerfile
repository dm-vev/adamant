# syntax=docker/dockerfile:1

ARG BUILDPLATFORM=linux/amd64
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH

ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=auto

WORKDIR /src

COPY go.mod go.sum ./
COPY net/go.mod ./net/
RUN go mod download

COPY . .
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/adamant . \
    && mkdir -p /out/data

FROM alpine:3.21

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/adamant /usr/local/bin/adamant
COPY --from=builder --chown=10001:10001 /out/data /data

WORKDIR /data
VOLUME ["/data"]

# 19132/tcp: NetherNet HTTP/HTTPS signaling
# 19132/udp: NetherNet WebRTC gameplay (+ optional Bedrock Query)
# 7551/udp:  optional LAN discovery
EXPOSE 19132/tcp 19132/udp 7551/udp

USER 10001:10001
ENTRYPOINT ["/usr/local/bin/adamant", "-headless"]

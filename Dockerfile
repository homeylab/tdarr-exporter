ARG BASE_IMAGE=golang
ARG BASE_IMAGE_TAG=1.21.5-alpine

FROM ${BASE_IMAGE}:${BASE_IMAGE_TAG} as builder

# Reference: https://gist.github.com/asukakenji/f15ba7e588ac42795f421b48b8aede63
# TARGETOS = GOOS (Go Operating System) target
# TARGETARCH = GOARCH (Go Architecture) target
# TARGETVARIANT = GOARM (Go ARM) target if using `TARGETARCH=arm` (https://github.com/goreleaser/goreleaser/issues/36)
ARG TARGETOS
ARG TARGETARCH
# ARG TARGETVARIANT=""

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH}
    # GOARM=${TARGETVARIANT}
# --no-cache option allows to not cache the index locally, which is useful for keeping containers small.
# --no-cache is equivalent to apk update && rm -rf /var/cache/apk/*
RUN apk add --no-cache ca-certificates dumb-init \
    && update-ca-certificates
WORKDIR /build
COPY . .
RUN go build -a -tags netgo -ldflags "-w -extldflags '-static'" -o exporter /build/cmd/exporter/.

FROM gcr.io/distroless/static:nonroot
LABEL \
    org.opencontainers.image.title="tdarr-exporter" \
    org.opencontainers.image.source="https://github.com/homeylab/tdarr-exporter"
USER nonroot:nonroot
COPY --from=builder --chown=nonroot:nonroot /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder --chown=nonroot:nonroot /build/exporter /tdar-exporter
COPY --from=builder --chown=nonroot:nonroot /usr/bin/dumb-init /dumb-init
# tini ensures that the default signal handlers work 
ENTRYPOINT [ "/dumb-init", "--", "/tdarr-exporter" ]
ARG BASE_IMAGE=golang
ARG BASE_IMAGE_TAG=1.22.6-alpine

ARG RUN_IMAGE=gcr.io/distroless/static
ARG RUN_IMAGE_TAG=nonroot

FROM ${BASE_IMAGE}:${BASE_IMAGE_TAG} as builder

# Reference: https://gist.github.com/asukakenji/f15ba7e588ac42795f421b48b8aede63
# TARGETOS = GOOS (Go Operating System) target
# TARGETARCH = GOARCH (Go Architecture) target
# TARGETVARIANT = GOARM (Go ARM) target if using `TARGETARCH=arm` (https://github.com/goreleaser/goreleaser/issues/36)
# - without it, you should specify `TARGETARCH/GOARCH` as `arm64`
# below are populated by `buildx` command with `--platform` option
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT=""

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT}
# --no-cache option allows to not cache the index locally, which is useful for keeping containers small.
# --no-cache is equivalent to apk update && rm -rf /var/cache/apk/*
RUN apk add --no-cache ca-certificates dumb-init \
    && update-ca-certificates
WORKDIR /build
COPY . .
# tags: `netgo` use pure go net package instead of the standard cgo net package
# - I do not believe this is needed if you have `CGO_ENABLED=0`
# ldlflags: `-w -s` disable debugging and `pprof` for minimal binary, 
# `-extldflags '-static'` means do not link against shared libraries
RUN go build -a -ldflags "-w -s -extldflags '-static' -X main.version=${VERSION} -X main.buildTime=${BUILDTIME} -X main.revision=${REVISION}" -o exporter /build/cmd/exporter/.

FROM ${RUN_IMAGE}:${RUN_IMAGE_TAG}
# OCI labels
LABEL \
    org.opencontainers.image.title="tdarr-exporter" \
    org.opencontainers.image.description="Prometheus exporter for tdarr" \
    org.opencontainers.image.source="https://github.com/homeylab/tdarr-exporter"
USER nonroot:nonroot
COPY --from=builder --chown=nonroot:nonroot /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder --chown=nonroot:nonroot /build/exporter /tdarr-exporter
COPY --from=builder --chown=nonroot:nonroot /usr/bin/dumb-init /dumb-init
# dumb-init ensures that the default signal handlers work
# https://github.com/Yelp/dumb-init
ENTRYPOINT [ "/dumb-init", "--", "/tdarr-exporter" ]
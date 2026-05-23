ARG BASE_IMAGE=golang
ARG BASE_IMAGE_TAG=1.26.3-alpine
ARG RUN_IMAGE=gcr.io/distroless/static
ARG RUN_IMAGE_TAG=nonroot

FROM ${BASE_IMAGE}:${BASE_IMAGE_TAG} AS builder

# BuildKit auto-populates these per --platform target
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT=""

# Build metadata, passed via --build-arg
ARG VERSION=""
ARG BUILDTIME=""
ARG REVISION=""

ENV CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT}

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build \
    # -trimpath: strip absolute build paths for reproducible builds
    -trimpath \
    # ldlflags: `-w -s` disable debugging and `pprof` for minimal binary,
    -ldflags "-w -s -X main.version=${VERSION} -X main.buildTime=${BUILDTIME} -X main.revision=${REVISION}" \
    -o exporter /build/cmd/exporter/.

FROM ${RUN_IMAGE}:${RUN_IMAGE_TAG}
# ARGs must be re-declared after FROM — they do not cross stage boundaries
ARG VERSION=""
ARG REVISION=""
LABEL \
    org.opencontainers.image.title="tdarr-exporter" \
    org.opencontainers.image.description="Prometheus exporter for tdarr" \
    org.opencontainers.image.source="https://github.com/homeylab/tdarr-exporter" \
    org.opencontainers.image.version="${VERSION}" \
    org.opencontainers.image.revision="${REVISION}"
USER nonroot:nonroot
COPY --from=builder --chown=nonroot:nonroot /build/exporter /tdarr-exporter
ENTRYPOINT ["/tdarr-exporter"]

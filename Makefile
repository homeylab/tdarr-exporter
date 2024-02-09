# GIT
GIT_REPO=github.com/homeylab/tdarr-exporter

# Module
MOD_NAME=${GIT_REPO}

# Docker
BASE_IMAGE=golang
BASE_IMAGE_TAG=1.22-alpine
RUN_IMAGE=gcr.io/distroless/static
RUN_IMAGE_TAG=nonroot

# for test - do not try to use externally
TEST_IMAGE_NAME=docker.homeylab.org/tdarr-exporter
TEST_IMAGE_TAG=1.0.0

IMAGE_ARCH=amd64
IMAGE_ARCH_ARM=arm64

# Golang
GOOS=linux
GOARCH_ARM=arm64
GOARM=""

tidy:
	go mod tidy

# Gofmt formats Go programs. It uses tabs for indentation and blanks for alignment.
# Alignment assumes that an editor is using a fixed-width font.
fmt:
	go fmt ./...

update_dep:
	go get -u ./...

lint:
	golangci-lint run

local_run:
	go run cmd/exporter/main.go --url=${TDARR_TEST_URL}

local_docker_build:
	docker buildx build \
	--load \
	--build-arg BASE_IMAGE=${BASE_IMAGE} \
	--build-arg BASE_IMAGE_TAG=${BASE_IMAGE_TAG} \
	--build-arg RUN_IMAGE=${RUN_IMAGE} \
	--build-arg RUN_TAG=${RUN_IMAGE_TAG} \
	--build-arg TARGETOS=${GOOS} \
	-t ${TEST_IMAGE_NAME}:${TEST_IMAGE_TAG} \
	--no-cache .

local_docker_run:
	docker run -i -p 9090:9090 -e TDARR_URL=${TDARR_URL} ${TEST_IMAGE_NAME}:${TEST_IMAGE_TAG}

docker_build:
	@docker buildx create --use --name=crossplat --node=crossplat && \
	docker buildx build \
	--platform linux/amd64,linux/arm64 \
	--output "type=image,push=false" \
	--build-arg BASE_IMAGE=${BASE_IMAGE} \
	--build-arg BASE_IMAGE_TAG=${BASE_IMAGE_TAG} \
	--build-arg RUN_IMAGE=${RUN_IMAGE} \
	--build-arg RUN_TAG=${RUN_IMAGE_TAG} \
	# --build-arg TARGETOS=${GOOS} \
	-t ${TEST_IMAGE_NAME}:${TEST_IMAGE_TAG} \
	--no-cache .

docker_build_latest:
	@docker buildx create --use --name=crossplat --node=crossplat && \
	docker buildx build \
	--platform linux/amd64,linux/arm64 \
	--output "type=image,push=true" \
	--build-arg BASE_IMAGE=${BASE_IMAGE} \
	--build-arg BASE_IMAGE_TAG=${BASE_IMAGE_TAG} \
	--build-arg DOCKER_WORK_DIR=${DOCKER_WORK_DIR} \
	--build-arg DOCKER_CONFIG_DIR=${DOCKER_CONFIG_DIR} \
	--build-arg DOCKER_EXPORT_DIR=${DOCKER_EXPORT_DIR} \
	-t ${TEST_IMAGE_NAME}:${TEST_IMAGE_TAG} \
	-t ${TEST_IMAGE_NAME}:latest \
	--no-cache .

docker_cleanup:
	docker buildx rm crossplat


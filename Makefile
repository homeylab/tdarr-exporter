# GIT
GIT_REPO=github.com/homeylab/tdarr-exporter

# Module
MOD_NAME=${GIT_REPO}

# Docker
BASE_IMAGE=golang
BASE_IMAGE_TAG=1.21.5-alpine
RUN_IMAGE=gcr.io/distroless/static
RUN_IMAGE_TAG=nonroot

IMAGE_NAME=homeylab/tdarr-exporter
IMAGE_TAG=0.0.1

IMAGE_ARCH=amd64
IMAGE_ARCH_ARM=arm64

# Golang
GOOS=linux
GOARCH_ARM=arm64
GOARM=7

tidy:
	go mod tidy

update_dep:
	go get -u ./...

lint:
	golangci-lint run

local_docker_build:
	docker buildx build \
	--load \
	--build-arg BASE_IMAGE=${BASE_IMAGE} \
	--build-arg BASE_IMAGE_TAG=${BASE_IMAGE_TAG} \
	--build-arg RUN_IMAGE=${RUN_IMAGE} \
	--build-arg RUN_TAG=${RUN_IMAGE_TAG} \
	--build-arg TARGETOS=${GOOS} \
	-t ${IMAGE_NAME}:test \
	--no-cache .

local_docker_run:
	docker run -i -p 9090:9090 -e TDARR_URL=${TDARR_URL} ${IMAGE_NAME}:test

docker_build:
	@docker buildx create --use --name=crossplat --node=crossplat && \
	docker buildx build \
	--platform linux/amd64,linux/arm64 \
	-output "type=image,push=true" \
	--build-arg BASE_IMAGE=${BASE_IMAGE} \
	--build-arg BASE_IMAGE_TAG=${BASE_IMAGE_TAG} \
	--build-arg RUN_IMAGE=${RUN_IMAGE} \
	--build-arg RUN_TAG=${RUN_IMAGE_TAG} \
	-t ${IMAGE_NAME}:${IMAGE_TAG} \
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
	-t ${IMAGE_NAME}:${IMAGE_TAG} \
	-t ${IMAGE_NAME}:latest \
	--no-cache .

docker_cleanup:
	docker buildx rm crossplat


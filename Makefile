# GIT
GIT_REPO=github.com/homeylab/tdarr-exporter

# Module
MOD_NAME=${GIT_REPO}

# Docker
BASE_IMAGE=golang
BASE_TAG=1.21.5-alpine
RUN_IMAGE=gcr.io/distroless/static
RUN_TAG=nonroot

IMAGE_NAME=homeylab/tdarr-exporter
IMAGE_TAG=0.0.1

IMAGE_ARCH=amd64
IMAGE_ARCH_ARM=arm64

tidy:
	go mod tidy

update_dep:
	go get -u ./...

lint:
.0
	golangci-lint run
0
docker_build: 
	docker buildx build \
	--build-arg BASE_IMAGE=${BASE_IMAGE} \
	--build-arg BASE_IMAGE_TAG=${BASE_IMAGE_TAG} \
	--build-arg RUN_IMAGE=${RUN_IMAGE} \
	--build-arg RUN_TAG=${RUN_TAG} \
	--build-arg TARGETOS=
	-t ${IMAGE_NAME}:${IMAGE_TAG} \
	--no-cache .

docker_build_latest:
	docker buildx build \
	--build-arg BASE_IMAGE=${BASE_IMAGE} \
	--build-arg BASE_IMAGE_TAG=${BASE_IMAGE_TAG} \
	--build-arg DOCKER_WORK_DIR=${DOCKER_WORK_DIR} \
	--build-arg DOCKER_CONFIG_DIR=${DOCKER_CONFIG_DIR} \
	--build-arg DOCKER_EXPORT_DIR=${DOCKER_EXPORT_DIR} \
	-t ${IMAGE_NAME}:${IMAGE_TAG} \
	-t ${IMAGE_NAME}:latest \
	--no-cache .
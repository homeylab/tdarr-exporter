# GIT
GIT_REPO=github.com/homeylab/tdarr-exporter

# Module
MOD_NAME=${GIT_REPO}

tidy:
	go mod tidy

update_dep:
	go get -u ./...
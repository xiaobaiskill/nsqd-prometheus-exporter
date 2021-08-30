BINARY_NAME := nsqd-prometheus-exporter
REPO := jinmz/$(BINARY_NAME)
VERSION := $(shell cat VERSION)
BUILD_FLAGS := -ldflags "-X main.Version=$(VERSION)"

default: check fmt deps test build

.PHONY: build
build:
	# Build project
	go build $(BUILD_FLAGS) -a -o $(BINARY_NAME) .

.PHONY: check
check:
	# Only continue if go is installed
	@go version || ( echo "Go not installed, exiting"; exit 1 )

.PHONY: docker
docker:
	docker build --build-arg Version='$(VERSION)' \
		-t $(REPO):$(VERSION) .
	docker image prune -f --filter label=stage=builder

.PHONY: release
release: docker
	docker push $(REPO):$(VERSION)


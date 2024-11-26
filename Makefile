
BUILD_DIR = ./sx1302
CGO_BUILD_LDFLAGS := -L$(shell pwd)/$(BUILD_DIR)/libloragw -L$(shell pwd)/$(BUILD_DIR)libloragw/lib -L$(shell pwd)/$(BUILD_DIR)/libtools
BIN_OUTPUT_PATH = bin
TOOL_BIN = bin/gotools/$(shell uname -s)-$(shell uname -m)
UNAME_S ?= $(shell uname -s)

.PHONY: build
build:
	rm -f $(BIN_OUTPUT_PATH)/lorawan
	CGO_LDFLAGS="$$CGO_LDFLAGS $(CGO_BUILD_LDFLAGS)" go build $(GO_BUILD_LDFLAGS) -o  $(BIN_OUTPUT_PATH)/lorawan main.go

module.tar.gz: sx1302 build
	rm -f $(BIN_OUTPUT_PATH)/module.tar.gz
	tar czf $(BIN_OUTPUT_PATH)/module.tar.gz $(BIN_OUTPUT_PATH)/lorawan

test: sx1302 build
	sudo apt install libnlopt-dev
	CGO_LDFLAGS="$$CGO_LDFLAGS $(CGO_BUILD_LDFLAGS)" go test -race -v ./...


sx1302: submodule
	cd sx1302/libtools && make
	cd sx1302/libloragw && make

submodule:
	git submodule init
	git submodule update

tool-install:
	GOBIN=`pwd`/$(TOOL_BIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint

gofmt:
	gofmt -w -s .

lint: gofmt tool-install
	go mod tidy
	$(TOOL_BIN)/golangci-lint run -v --fix --config=./etc/.golangci.yaml


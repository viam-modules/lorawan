
BUILD_DIR = ./sx1302
CGO_BUILD_LDFLAGS := -L$(shell pwd)/$(BUILD_DIR)/libloragw -L$(shell pwd)/$(BUILD_DIR)libloragw/lib -L$(shell pwd)/$(BUILD_DIR)/libtools

.PHONY: all
all:
	CGO_LDFLAGS="$$CGO_LDFLAGS $(CGO_BUILD_LDFLAGS)" go build $(GO_BUILD_LDFLAGS) -o mymod main.go

test:
	CGO_LDFLAGS="$$CGO_LDFLAGS $(CGO_BUILD_LDFLAGS)" go test -v gateway/gateway


BUILD_DIR = ./sx1302_hal
CGO_BUILD_LDFLAGS := -L$(shell pwd)/$(BUILD_DIR)/libloragw -L$(shell pwd)/$(BUILD_DIR)libloragw/lib -L$(shell pwd)/$(BUILD_DIR)/libtools -L$(shell pwd)/$(BUILD_DIR)/packet_forwarder -L$(shell pwd)/$(BUILD_DIR)/packet_forwarder

.PHONY: all
all:
	CGO_LDFLAGS="$$CGO_LDFLAGS $(CGO_BUILD_LDFLAGS)" go build $(GO_BUILD_LDFLAGS) -o mymod main.go

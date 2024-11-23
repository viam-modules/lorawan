
BUILD_DIR = ./sx1302
CGO_BUILD_LDFLAGS := -L$(shell pwd)/$(BUILD_DIR)/libloragw -L$(shell pwd)/$(BUILD_DIR)libloragw/lib -L$(shell pwd)/$(BUILD_DIR)/libtools

.PHONY: all
all: sx1302/libloragw/libloragw.a
	CGO_LDFLAGS="$$CGO_LDFLAGS $(CGO_BUILD_LDFLAGS)" go build $(GO_BUILD_LDFLAGS) -o mymod main.go

test: sx1302/libloragw/libloragw.a
	CGO_LDFLAGS="$$CGO_LDFLAGS $(CGO_BUILD_LDFLAGS)" go test -v gateway/gateway


sx1302/libloragw/libloragw.a: sx1302/libloragw sx1302/libloragw/library.cfg
	cd sx1302/libtools && make
	cd sx1302/libloragw && make

sx1302/libloragw:
	git submodule init
	git submodule update

lint:
	gofmt -w .

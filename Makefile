.PHONY: build test fmt

BIN_DIR := bin
export CGO_ENABLED ?= 1

# Optional librsvg CGO (breeze/adwaita path-heavy SVGs). Without
# librsvg2-dev the simple raster in internal/icontheme/svg.go is used.
RSVG_FLAGS :=
ifeq ($(shell pkg-config --exists librsvg-2.0 && echo yes),yes)
RSVG_FLAGS := -tags=librsvg
endif

build:
	mkdir -p $(BIN_DIR)
	go build $(RSVG_FLAGS) -o $(BIN_DIR)/worldr-shell ./cmd/worldr-shell
	go build $(RSVG_FLAGS) -o $(BIN_DIR)/worldr-session ./cmd/worldr-session

test:
	go test $(RSVG_FLAGS) ./...

fmt:
	gofmt -w .

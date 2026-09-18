.PHONY: build install test test-static test-gpu test-compat test-integration test-nested test-race test-soak test-soak-contract test-ci-soak qualify-tty preview fmt shaders

BIN_DIR := bin
PREFIX ?= /usr/local
DESTDIR ?=
WORLDR_PYTHON ?= python3
export CGO_ENABLED ?= 1

# Optional unpacked Khronos headers when the system Vulkan SDK lacks headers.
ifneq ($(VULKAN_HEADERS),)
export CGO_CFLAGS := $(CGO_CFLAGS) -I$(VULKAN_HEADERS)/include
endif

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/worldr-shell ./cmd/worldr-shell
	go build -o $(BIN_DIR)/worldr-session ./cmd/worldr-session

install: build
	install -Dm755 $(BIN_DIR)/worldr-shell $(DESTDIR)$(PREFIX)/bin/worldr-shell
	install -Dm755 $(BIN_DIR)/worldr-session $(DESTDIR)$(PREFIX)/bin/worldr-session
	install -Dm644 contrib/wayland-sessions/worldr.desktop $(DESTDIR)$(PREFIX)/share/wayland-sessions/worldr.desktop
	install -Dm644 contrib/applications/worldr.desktop $(DESTDIR)$(PREFIX)/share/applications/worldr.desktop

test:
	go test ./...

test-static:
	@test -z "$$(gofmt -l $$(rg --files --glob '*.go'))"
	go vet ./...
	git diff --check

test-gpu:
	WORLDR_TEST_GPU=1 go test ./...

test-compat:
	WORLDR_TEST_APPS=1 WORLDR_TEST_GPU=1 go test ./...

# Requires the real helper programs and a Vulkan device listed in docs/RUN-ABOX.md.
test-integration:
	WORLDR_TEST_APPS=1 WORLDR_TEST_GPU=1 WORLDR_TEST_MEDIA=1 WORLDR_TEST_XWAYLAND=1 WORLDR_TEST_TOOLKITS=1 WORLDR_TEST_DMABUF=1 go test ./...

# Uses a private Wayland compositor; scripted input cannot reach the host desktop.
test-nested:
	$(WORLDR_PYTHON) -c 'from pathlib import Path; [path.unlink(missing_ok=True) for path in (Path("dist/nested-input-recovery.json"), Path("dist/nested-input-recovery.png"))]'
	WORLDR_TEST_NESTED=1 WORLDR_CAPTURE_DIR="$(CURDIR)/dist" go test -v ./internal/app -run TestIsolatedNestedWorkspaceInputAndRecoveryGPU
	$(WORLDR_PYTHON) scripts/soak-verify.py run dist/nested-input-recovery.json dist/nested-input-recovery.png nested-private 1280 800

test-race:
	go test -race ./...

# Defaults to three 20-minute restart/resume cycles. Override DURATION/CYCLES.
test-soak:
	./scripts/soak.sh

# Fast corruption and exact-checkpoint tests for the soak evidence validator.
test-soak-contract:
	bash -n scripts/soak.sh
	$(WORLDR_PYTHON) scripts/soak-verify.py self-test

# A short restart/resume run for continuous integration. It follows the same
# artifact and exact-restore checks as the one-hour qualification workload.
test-ci-soak:
	DURATION=5s CYCLES=2 ./scripts/soak.sh

# Must be invoked from a spare Linux TTY; the script refuses graphical sessions.
qualify-tty:
	./scripts/try-tty.sh

preview: build
	./$(BIN_DIR)/worldr-shell --backend=headless --demo --frames=1 --snapshot=dist/worldr-preview.png

shaders:
	python3 internal/platform/linux/native/shaders/generate.py

fmt:
	gofmt -w $$(rg --files --glob '*.go')

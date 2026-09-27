BINARY_NAME=blitz
BUILD_DIR=bin
VERSION ?= $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)
LDFLAGS=-s -w -X main.version=$(VERSION)
GOFLAGS_BUILD=-trimpath -buildvcs=false

.PHONY: all build install test test-race vet lint vulncheck check clean cross-compile tidy snapshot release-check proto proto-check desktop desktop-check desktop-package

all: build

# blitz (the CLI, apps/cli) and blitzd (the service, apps/service), which
# `blitz service install` expects beside it.
build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build $(GOFLAGS_BUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./apps/cli
	CGO_ENABLED=0 go build $(GOFLAGS_BUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/blitzd ./apps/service
	@ln -sf $(BINARY_NAME) $(BUILD_DIR)/blz
	@echo "✅ Built $(BUILD_DIR)/$(BINARY_NAME), $(BUILD_DIR)/blitzd (and $(BUILD_DIR)/blz)"

# Installs blitz, blitzd, and blz beside them, into $$GOBIN (or $$GOPATH/bin).
install: build
	@dir="$$(go env GOBIN)"; [ -n "$$dir" ] || dir="$$(go env GOPATH)/bin"; \
	mkdir -p "$$dir" && cp $(BUILD_DIR)/$(BINARY_NAME) $(BUILD_DIR)/blitzd "$$dir/" && ln -sf $(BINARY_NAME) "$$dir/blz" && \
	echo "✅ Installed $$dir/$(BINARY_NAME), $$dir/blitzd and $$dir/blz"

test:
	CGO_ENABLED=0 go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

# staticcheck and govulncheck are pinned in tools/go.mod. Every package's
# tests also fail on a leaked goroutine (goleak, in each leak_test.go).
lint:
	go tool -modfile=tools/go.mod staticcheck ./...

vulncheck:
	go tool -modfile=tools/go.mod govulncheck ./...

check: vet lint vulncheck test-race

snapshot:
	goreleaser release --snapshot --clean

release-check:
	goreleaser check

tidy:
	go mod tidy

# The service API: proto/blitz/v1/*.proto -> Go beside them, TypeScript in
# apps/desktop/web/src/gen. The tools are pinned in tools/go.mod.
BUF=go tool -modfile=tools/go.mod buf

# protoc-gen-es, for the desktop app's TypeScript, comes from its page.
apps/desktop/web/node_modules: apps/desktop/web/pnpm-lock.yaml
	cd apps/desktop/web && pnpm install --frozen-lockfile
	touch $@

proto: apps/desktop/web/node_modules
	$(BUF) lint
	$(BUF) format -w
	$(BUF) generate

# Fails when the protos aren't formatted or the generated code is out of date.
proto-check: apps/desktop/web/node_modules
	$(BUF) lint
	$(BUF) format --exit-code -d
	$(BUF) generate
	git diff --exit-code -- proto apps/desktop/web/src/gen
	test -z "$$(git ls-files --others --exclude-standard -- proto apps/desktop/web/src/gen)"

clean:
	rm -rf $(BUILD_DIR)

cross-compile: clean
	@mkdir -p $(BUILD_DIR)
	@for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do \
		os=$${t%/*}; arch=$${t#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "Building for $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build $(GOFLAGS_BUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-$$os-$$arch$$ext ./apps/cli || exit 1; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build $(GOFLAGS_BUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/blitzd-$$os-$$arch$$ext ./apps/service || exit 1; \
	done
	@echo "✅ All cross-compiled binaries created in $(BUILD_DIR)/"

# The desktop app (apps/desktop; its page is apps/desktop/web):
# apps/desktop/packaging/bin. Needs cgo, pnpm, and on Linux webkit2gtk.
# The Wails CLI is pinned in tools/go.mod.
WAILS=go tool -modfile=../../tools/go.mod wails

DESKTOP_BIN=apps/desktop/packaging/bin
DESKTOP_APP=$(DESKTOP_BIN)/Blitz.app

# The CLI and the service are bundled next to the app's binary, where the
# app looks for the CLI to install the service (and the CLI for blitzd);
# on macOS the app is then signed again (ad hoc), since adding files breaks
# Wails's signature. The app's executable is blitz-desktop (wails.json
# outputfilename), never "Blitz": macOS file systems ignore case, so the
# CLI would overwrite it.
desktop: apps/desktop/web/node_modules
	cd apps/desktop && CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0 $(WAILS) build -clean
	@if [ -d "$(DESKTOP_APP)" ]; then dir="$(DESKTOP_APP)/Contents/MacOS"; else dir="$(DESKTOP_BIN)"; fi; \
	CGO_ENABLED=0 go build $(GOFLAGS_BUILD) -ldflags="$(LDFLAGS)" -o "$$dir/blitz" ./apps/cli && \
	CGO_ENABLED=0 go build $(GOFLAGS_BUILD) -ldflags="$(LDFLAGS)" -o "$$dir/blitzd" ./apps/service && \
	test -x "$$dir/blitz-desktop" || { echo "the bundled CLI replaced the app's executable" >&2; exit 1; }; \
	if [ -d "$(DESKTOP_APP)" ]; then \
		test "$$(ls "$$dir" | wc -l)" -eq 3 || { echo "unexpected files in $$dir" >&2; exit 1; }; \
		codesign --force --deep --sign - "$(DESKTOP_APP)"; \
	fi
	@echo "✅ Built $(DESKTOP_BIN) (with blitz and blitzd bundled)"

# The release packages (apps/desktop/packaging/dist): a universal, signed
# and notarised disk image on macOS; a .deb on Linux. Signing needs the
# variables described in apps/desktop/packaging/package.sh.
desktop-package:
	apps/desktop/packaging/package.sh $(VERSION)

desktop-check: apps/desktop/web/node_modules
	cd apps/desktop/web && pnpm test && pnpm run build
	go vet ./apps/desktop && go test -race ./apps/desktop

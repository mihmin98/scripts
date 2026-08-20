GO 	:= go
PKG	:= ./cmd/...
ALL_PKG	:= ./...

OUTDIR 					:= build
OUTDIR_LINUX_AMD64_V1 	:= $(OUTDIR)/linux-amd64-v1
OUTDIR_LINUX_AMD64_V4 	:= $(OUTDIR)/linux-amd64-v4
OUTDIR_WINDOWS_AMD64 	:= $(OUTDIR)/windows-amd64
LDFLAGS 				:= -s -w
BUILDFLAGS 				:= -trimpath -ldflags="$(LDFLAGS)"

LINUX_INSTALL_DIR_AMD64 := $(HOME)/.local/bin

.PHONY: all create-build-dir build build-linux build-linux-amd64-v4 build-windows \
	install-linux-amd64-v4 check test clean

all: build

create-build-dir:
	@mkdir -p $(OUTDIR_LINUX_AMD64_V1) $(OUTDIR_LINUX_AMD64_V4) $(OUTDIR_WINDOWS_AMD64)

build-linux: create-build-dir
	GOOS=linux GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0 $(GO) build -o $(OUTDIR_LINUX_AMD64_V1) $(BUILDFLAGS) $(PKG)

build-linux-amd64-v4: create-build-dir
	GOOS=linux GOARCH=amd64 GOAMD64=v4 CGO_ENABLED=0 $(GO) build -o $(OUTDIR_LINUX_AMD64_V4) $(BUILDFLAGS) $(PKG)

build-windows: create-build-dir
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o $(OUTDIR_WINDOWS_AMD64) $(BUILDFLAGS) $(PKG)

build: build-linux-amd64-v4 build-linux build-windows

install-linux-amd64-v4: build-linux-amd64-v4
	@mkdir -p $(LINUX_INSTALL_DIR_AMD64)
	@cp -v $(OUTDIR_LINUX_AMD64_V4)/* $(LINUX_INSTALL_DIR_AMD64)

check:
	gofmt -l .
	$(GO) vet $(ALL_PKG)

test:
	$(GO) test $(ALL_PKG)

clean:
	@rm -rfv $(OUTDIR)

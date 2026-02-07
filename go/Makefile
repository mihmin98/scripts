GO 	:= go
PKG	:= ./...

OUTDIR 					:= build
OUTDIR_LINUX 			:= $(OUTDIR)/linux
OUTDIR_LINUX_AMD64_V4 	:= $(OUTDIR)/linux-amd64-v4
OUTDIR_WINDOWS 			:= $(OUTDIR)/windows
LDFLAGS 				:= -s -w

LINUX_INSTALL_DIR_AMD64 := $(HOME)/.local/bin

.PHONY: all build clean

all:
	build

create-build-dir:
	@mkdir -p $(OUTDIR)
	@mkdir -p $(OUTDIR_LINUX)
	@mkdir -p $(OUTDIR_LINUX_AMD64_V4)
	@mkdir -p $(OUTDIR_WINDOWS)

build-linux: create-build-dir
	GOOS=linux GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0 $(GO) build -o $(OUTDIR_LINUX) -ldflags="$(LDFLAGS)" $(PKG)

build-linux-amd64-v4: create-build-dir
	GOOS=linux GOARCH=amd64 GOAMD64=v4 CGO_ENABLED=0 $(GO) build -o $(OUTDIR_LINUX_AMD64_V4) -ldflags="$(LDFLAGS)" $(PKG)

build-windows:create-build-dir
	GOOS=windows GOARCH=amd64 $(GO) build -o $(OUTDIR_WINDOWS) -ldflags="$(LDFLAGS)" $(PKG)

build: build-linux-amd64-v4 build-linux build-windows

install-linux-amd64-v4: build-linux-amd64-v4
	@cp -v $(OUTDIR_LINUX_AMD64_V4)/* $(LINUX_INSTALL_DIR_AMD64)

clean:
	@rm -rv $(OUTDIR)

test:
	@echo "test not implemented"

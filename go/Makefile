GO 	:= go
PKG	:= ./...

OUTDIR 			:= build
OUTDIR_LINUX 	:= $(OUTDIR)/linux
OUTDIR_WINDOWS 	:= $(OUTDIR)/windows
LDFLAGS 		:= -s -w

LINUX_INSTALL_DIR := $(HOME)/.local/bin

.PHONY: all build clean

all:
	build

create-build-dir:
	@mkdir -p $(OUTDIR)
	@mkdir -p $(OUTDIR_LINUX)
	@mkdir -p $(OUTDIR_WINDOWS)

build-linux: create-build-dir
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o $(OUTDIR_LINUX) -ldflags="$(LDFLAGS)" $(PKG)

build-windows:create-build-dir
	GOOS=windows GOARCH=amd64 $(GO) build -o $(OUTDIR_WINDOWS) -ldflags="$(LDFLAGS)" $(PKG)

build: build-linux build-windows

install-linux: build-linux
	@cp -v $(OUTDIR_LINUX)/* $(LINUX_INSTALL_DIR)

clean:
	@rm -rv $(OUTDIR)

test:
	@echo "test not implemented"

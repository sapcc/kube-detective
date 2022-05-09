DATE    = $(shell date +%Y%m%d%H%M) 
VERSION = v$(DATE) 
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= $(shell go env GOARCH)

LDFLAGS     := -X github.com/sapcc/kube-detective/pkg/detective.VERSION=$(VERSION) 
GOFLAGS     := -ldflags "$(LDFLAGS)"

BINARIES := detective
CMDDIR   := cmd
PKGDIR   := pkg
PACKAGES := $(shell find $(CMDDIR) $(PKGDIR) -type d)
GOFILES  := $(addsuffix /*.go,$(PACKAGES))
GOFILES  := $(wildcard $(GOFILES))            

.PHONY: FORCE all 

all: $(BINARIES:%=bin/$(GOOS)/$(GOARCH)/%)

bin/$(GOOS)/$(GOARCH)/%: $(GOFILES) Makefile
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -v -o bin/$(GOOS)/$(GOARCH)/kube-detective ./cmd/$*

release: FORCE binaries gh-release

binaries: 
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -v -o bin/$(BINARIES)_linux_amd64 ./cmd/detective
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -v -o bin/$(BINARIES)_windows_amd64.exe ./cmd/detective
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -v -o bin/$(BINARIES)_darwin_amd64 ./cmd/detective
	GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -v -o bin/$(BINARIES)_darwin_arm64 ./cmd/detective

gh-release:
	gh release create $(VERSION) bin/*

clean: FORCE
	rm -rf bin/*


DATE    = $(shell date +%Y%m%d%H%M) 
VERSION = v$(DATE) 
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= amd64

LDFLAGS     := -X github.com/sapcc/kube-detective/pkg/detective.VERSION=$(VERSION) 
GOFLAGS     := -ldflags "$(LDFLAGS)"

BINARIES := detective
CMDDIR   := cmd
PKGDIR   := pkg
PACKAGES := $(shell find $(CMDDIR) $(PKGDIR) -type d)
GOFILES  := $(addsuffix /*.go,$(PACKAGES))
GOFILES  := $(wildcard $(GOFILES))            

.PHONY: all clean 

all: $(BINARIES:%=bin/$(GOOS)/$(GOARCH)/%)

bin/$(GOOS)/$(GOARCH)/%: $(GOFILES) Makefile
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -v -i -mod vendor -o bin/$(GOOS)/$(GOARCH)/kube-detective ./cmd/$*

release: $(GOFILES) Makefile
	GOOS=linux GOARCH=$(GOARCH) go build $(GOFLAGS) -v -i -mod vendor -o bin/$(BINARIES)_linux_amd64 ./cmd/detective
	GOOS=windows GOARCH=$(GOARCH) go build $(GOFLAGS) -v -i -mod vendor -o bin/$(BINARIES)_windows_amd64.exe ./cmd/detective
	GOOS=darwin GOARCH=$(GOARCH) go build $(GOFLAGS) -v -i -mod vendor -o bin/$(BINARIES)_darwin_amd64 ./cmd/detective

clean:
	rm -rf bin/*


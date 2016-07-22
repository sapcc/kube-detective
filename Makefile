DATE    = $(shell date +%Y%m%d%H%M) 
VERSION = v$(DATE) 
GOOS    ?= darwin
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
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -v -i -o bin/$(GOOS)/$(GOARCH)/$* ./cmd/$*

clean:
	rm -rf bin/*


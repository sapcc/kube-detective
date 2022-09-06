IMAGE	:= keppel.eu-de-1.cloud.sap/ccloud/kube-detective
DATE    = $(shell date +%Y%m%d%H%M)
VERSION = v$(DATE) 
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= $(shell go env GOARCH)

LDFLAGS     := -X github.com/sapcc/kube-detective/internal/detective.VERSION=$(VERSION)
GOFLAGS     := -ldflags "$(LDFLAGS)"

BINARIES := detective
CMDDIR   := cmd
PKGDIR   := internal
PACKAGES := $(shell find $(CMDDIR) $(PKGDIR) -type d)
GOFILES  := $(addsuffix /*.go,$(PACKAGES))
GOFILES  := $(wildcard $(GOFILES))            

.PHONY: FORCE all 

all: $(BINARIES:%=bin/$(GOOS)/$(GOARCH)/%)

bin/$(GOOS)/$(GOARCH)/%: $(GOFILES) Makefile
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -v -o bin/$(GOOS)/$(GOARCH)/kube-detective ./cmd/$*

release: FORCE binaries gh-release

binaries: 
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -v -o bin/$(BINARIES)_linux_amd64 ./cmd/detective
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -v -o bin/$(BINARIES)_windows_amd64.exe ./cmd/detective
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -v -o bin/$(BINARIES)_darwin_amd64 ./cmd/detective
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -v -o bin/$(BINARIES)_darwin_arm64 ./cmd/detective

run:
	CGO_ENABLED=0 go build -o kube-detective cmd/detective/main.go
	./kube-detective --test-image keppel.eu-de-1.cloud.sap/ccloud-dockerhub-mirror/sapcc/serve-hostname-amd64:1.2-alpine --externalips --externalCIDR=169.253.0.0/24

gh-release:
	gh release create $(VERSION) bin/detective*

build:
	docker build -t $(IMAGE):$(VERSION) .

push: build
	docker push ${IMAGE}:${VERSION}

docker-push-mac:
	docker buildx build  --platform linux/amd64 . -t ${IMAGE}:${VERSION} --push

clean: FORCE
	rm -rf bin/*


BINARY  := pix-sandbox
PKG     := ./cmd/pix-sandbox
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
IMAGE   ?= ghcr.io/arinellidu/pix-sandbox-pay

.DEFAULT_GOAL := help

.PHONY: help run generate test test-race build docker-build docker-run tidy fmt vet lint clean

## help: list the targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'

## run: start the sandbox on :8080
run:
	go run $(PKG)

## generate: rebuild the console's templates (commit what it writes)
generate:
	go tool templ generate

## test: run the full test suite
test:
	go test ./...

## test-race: same, under the race detector (needs CGO and a C toolchain)
test-race:
	CGO_ENABLED=1 go test -race ./...

## build: produce a static, CGO-free binary in ./bin
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

## docker-build: build the distroless image
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

## docker-run: run the image on :8080
docker-run:
	docker run --rm -p 8080:8080 $(IMAGE):$(VERSION)

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

lint: fmt vet

clean:
	rm -rf bin data

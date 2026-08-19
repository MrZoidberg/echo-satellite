# Get the latest commit branch, hash, and date
TAG=$(shell git describe --tags --abbrev=0 --exact-match 2>/dev/null)
BRANCH=$(if $(TAG),$(TAG),$(shell git rev-parse --abbrev-ref HEAD 2>/dev/null))
HASH=$(shell git rev-parse --short=7 HEAD 2>/dev/null)
TIMESTAMP=$(shell git log -1 --format=%ct HEAD 2>/dev/null | xargs -I{} date -u -r {} +%Y%m%dT%H%M%S)
GIT_REV=$(shell printf "%s-%s-%s" "$(BRANCH)" "$(HASH)" "$(TIMESTAMP)")
REV=$(if $(filter --,$(GIT_REV)),latest,$(GIT_REV))

LDFLAGS=-X main.revision=$(REV) -s -w
CMDS=echod gateway echoctl dotsim

all: test build

build:
	@for cmd in $(CMDS); do \
		echo "building $$cmd"; \
		go build -ldflags "$(LDFLAGS)" -o .bin/$$cmd ./cmd/$$cmd || exit 1; \
	done

# device build: pure-Go static binary for the Echo Dot (see docs/DESIGN.md 22)
build-device:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o .bin/linux_arm64/echod ./cmd/echod

test:
	go clean -testcache
	go test -race -coverprofile=coverage.out ./...
	grep -v -E "_mock.go|/mocks/" coverage.out > coverage_no_mocks.out
	go tool cover -func=coverage_no_mocks.out
	rm coverage.out coverage_no_mocks.out

race:
	go test -race -timeout=100s ./...

lint:
	golangci-lint run

fmt:
	gofmt -l -w .

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

version:
	@echo "branch: $(BRANCH), hash: $(HASH), timestamp: $(TIMESTAMP)"
	@echo "revision: $(REV)"

.PHONY: all build build-device test race lint fmt fmt-check version

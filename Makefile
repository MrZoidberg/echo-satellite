# Get the latest commit branch, hash, and date
TAG=$(shell git describe --tags --abbrev=0 --exact-match 2>/dev/null)
BRANCH=$(if $(TAG),$(TAG),$(shell git rev-parse --abbrev-ref HEAD 2>/dev/null))
HASH=$(shell git rev-parse --short=7 HEAD 2>/dev/null)
TIMESTAMP=$(shell ts=$$(git log -1 --format=%ct HEAD 2>/dev/null); date -u -d @$$ts +%Y%m%dT%H%M%S 2>/dev/null || date -u -r $$ts +%Y%m%dT%H%M%S 2>/dev/null)
GIT_REV=$(shell printf "%s-%s-%s" "$(BRANCH)" "$(HASH)" "$(TIMESTAMP)")
REV=$(if $(filter --,$(GIT_REV)),latest,$(GIT_REV))

LDFLAGS=-X main.revision=$(REV) -s -w
CMDS=echod gateway echoctl dotsim
ADB ?= adb
DEVICE_SERIAL ?=
DEVICE_ARGS ?= --dbg
DEVICE_TMP := /data/local/tmp/echod
ADB_DEVICE := $(ADB) $(if $(DEVICE_SERIAL),-s $(DEVICE_SERIAL),)

all: test build

build:
	@for cmd in $(CMDS); do \
		echo "building $$cmd"; \
		go build -ldflags "$(LDFLAGS)" -o .bin/$$cmd ./cmd/$$cmd || exit 1; \
	done

# device build: pure-Go static binary for the Echo Dot (see docs/DESIGN.md 22)
build-device:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o .bin/linux_arm64/echod ./cmd/echod

build-device-ctl:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o .bin/linux_arm64/echoctl ./cmd/echoctl

build-device-noasm:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags noasm -ldflags "$(LDFLAGS)" -o .bin/linux_arm64/echod-noasm ./cmd/echod

check-portability:
	GOOS=darwin GOARCH=arm64 go build ./...
	GOOS=linux GOARCH=arm64 go build ./...

bench:
	go test -bench . -benchmem ./...

device-check:
	@state=$$($(ADB_DEVICE) get-state | tr -d '\r'); \
		[ "$$state" = device ] || { echo "ADB state is '$$state', want 'device'"; exit 1; }; \
		echo "state: $$state"
	@root=$$($(ADB_DEVICE) shell 'su -c id' | tr -d '\r'); \
		echo "root: $$root"; \
		printf '%s\n' "$$root" | grep -q 'uid=0(root)' || \
		{ echo "device shell cannot gain root with su"; exit 1; }
	@product=$$($(ADB_DEVICE) shell getprop ro.product.device | tr -d '\r'); \
		[ "$$product" = biscuit ] || { echo "device is '$$product', want 'biscuit'"; exit 1; }; \
		echo "device: $$product"
	@abi=$$($(ADB_DEVICE) shell getprop ro.product.cpu.abi | tr -d '\r'); \
		[ "$$abi" = arm64-v8a ] || { echo "device ABI is '$$abi', want 'arm64-v8a'"; exit 1; }; \
		echo "ABI: $$abi"
	@selinux=$$($(ADB_DEVICE) shell getenforce | tr -d '\r'); \
		[ "$$selinux" = Permissive ] || { echo "SELinux is '$$selinux', want 'Permissive'"; exit 1; }; \
		echo "SELinux: $$selinux"

push-device: device-check build-device
	$(ADB_DEVICE) push .bin/linux_arm64/echod $(DEVICE_TMP)
	$(ADB_DEVICE) shell chmod 755 $(DEVICE_TMP)
	$(ADB_DEVICE) shell $(DEVICE_TMP) --version

run-device: push-device
	@$(ADB_DEVICE) shell su -c '$(DEVICE_TMP) $(DEVICE_ARGS)'; status=$$?; \
		if [ $$status -ne 0 ] && [ $$status -ne 58 ]; then exit $$status; fi; \
		$(MAKE) --no-print-directory device-stopped

device-stopped:
	@state=$$($(ADB_DEVICE) get-state | tr -d '\r'); \
		[ "$$state" = device ] || { echo "ADB state is '$$state' after run"; exit 1; }
	@output=$$($(ADB_DEVICE) shell ps) || exit $$?; \
		pids=$$(printf '%s\n' "$$output" | tr -d '\r' | awk '$$NF == "$(DEVICE_TMP)" { print $$2 }'); \
		[ -z "$$pids" ] || { echo "echod is still running as PID(s): $$pids"; exit 1; }; \
		echo "echod stopped"

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

.PHONY: all build build-device build-device-ctl build-device-noasm check-portability bench device-check push-device run-device device-stopped test race lint fmt fmt-check version

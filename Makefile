BINARY  := zero
GO      ?= go
VERSION ?= 0.8.0
LDFLAGS ?= -s -w -X main.version=$(VERSION)
MODULES := zero-beat-core zero-beat-tui

.PHONY: all build run demo test vet fmt fmt-check tidy check check-modules clean
all: check build
build:
	$(GO) -C zero-beat-tui build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/$(BINARY) ./cmd/$(BINARY)
run:
	$(GO) -C zero-beat-tui run ./cmd/$(BINARY)
demo:
	$(GO) -C zero-beat-tui run ./cmd/$(BINARY) demo
test:
	@for module in $(MODULES); do $(GO) -C $$module test ./... || exit; done
vet:
	@for module in $(MODULES); do $(GO) -C $$module vet ./... || exit; done
fmt:
	gofmt -w $(MODULES)
fmt-check:
	@test -z "$$(gofmt -l $(MODULES))" || { gofmt -l $(MODULES); exit 1; }
tidy:
	GO=$(GO) python3 scripts/check-modules.py --tidy
check: fmt-check vet test
check-modules:
	GO=$(GO) python3 scripts/check-modules.py
clean:
	rm -rf bin dist

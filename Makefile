BINARY  := zero
GO      ?= go
VERSION ?= 0.5.0
LDFLAGS ?= -s -w -X main.version=$(VERSION)

.PHONY: all build run demo test vet fmt fmt-check tidy check clean

all: check build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

run:
	$(GO) run ./cmd/$(BINARY)

# 不联网打印一帧示例界面，用于检查布局
demo:
	$(GO) run ./cmd/$(BINARY) demo

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

fmt-check:
	@files="$$(git ls-files --cached --others --exclude-standard '*.go')"; \
	test -z "$$(gofmt -l $$files)" || { echo "以下文件未格式化："; gofmt -l $$files; exit 1; }

tidy:
	$(GO) mod tidy

check: fmt-check vet test

clean:
	rm -rf bin

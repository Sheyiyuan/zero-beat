BINARY := zero
GO     ?= go

.PHONY: all build run demo test vet fmt fmt-check tidy check clean

all: check build

build:
	$(GO) build -trimpath -o bin/$(BINARY) ./cmd/$(BINARY)

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
	@test -z "$$(gofmt -l .)" || { echo "以下文件未格式化："; gofmt -l .; exit 1; }

tidy:
	$(GO) mod tidy

check: fmt-check vet test

clean:
	rm -rf bin

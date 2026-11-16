# zero-beat 伞仓 Makefile（仓库根目录）
#
# 子模块：zero-beat-core / zero-beat-tui / zero-beat-gui / zero-beat-site
# 所有目标从本目录执行；TUI 通过 Go module 依赖 core，本地由 go.work 联调。

SHELL := /bin/bash
BINARY := zero
VERSION ?= 0.8.0
GO ?= go
PYTHON ?= python3
PNPM ?= pnpm
GIT ?= git
LDFLAGS ?= -s -w -X main.version=$(VERSION)

CORE := zero-beat-core
TUI := zero-beat-tui
GUI := zero-beat-gui
SITE := zero-beat-site
MODULES := $(CORE) $(TUI)

.DEFAULT_GOAL := all

.PHONY: all help submodules version build run demo test vet fmt fmt-check tidy \
	check check-modules check-deps check-docs check-site \
	site-install site-sync site-build site-dev \
	release-check release clean distclean

all: check build

help:
	@printf '%s\n' \
		'zero-beat 伞仓开发入口（仓库根目录）' \
		'' \
		'  make submodules       初始化/更新全部子模块' \
		'  make help             显示本帮助' \
		'  make all              check + build（默认）' \
		'  make check            fmt-check + vet + 两模块 test + 文档链接 + git diff --check' \
		'  make build            构建 bin/zero（TUI 子模块）' \
		'  make run              直接运行 TUI' \
		'  make demo             打印一帧示例界面' \
		'  make test / vet       只跑测试 / go vet' \
		'  make fmt / fmt-check  格式化 / 检查格式' \
		'  make tidy             用临时模块代理整理 go.mod/go.sum' \
		'  make check-modules    GOWORK=off 独立消费两个 module' \
		'  make check-deps       四平台 core 依赖边界与编译' \
		'  make check-docs       Markdown 链接 + 空白检查' \
		'  make site-install     安装站点依赖' \
		'  make site-build       同步 docs/ 并构建站点' \
		'  make verify           check + check-modules + check-deps + check-site' \
		'  make release-check    goreleaser check' \
		'  make release          goreleaser release --clean' \
		'  make clean / distclean 清构建产物 / 连站点 node_modules 一起清'

submodules:
	$(GIT) submodule update --init --recursive

version:
	@if [ -x bin/$(BINARY) ]; then ./bin/$(BINARY) version; else $(GO) -C $(TUI) run ./cmd/$(BINARY) version; fi

build:
	@mkdir -p bin
	$(GO) -C $(TUI) build -trimpath -ldflags "$(LDFLAGS)" -o $(CURDIR)/bin/$(BINARY) ./cmd/$(BINARY)

run:
	$(GO) -C $(TUI) run ./cmd/$(BINARY)

demo:
	$(GO) -C $(TUI) run ./cmd/$(BINARY) demo

test:
	@for module in $(MODULES); do \
		echo "==> test $$module"; \
		$(GO) -C $$module test ./... || exit 1; \
	done

vet:
	@for module in $(MODULES); do \
		echo "==> vet $$module"; \
		$(GO) -C $$module vet ./... || exit 1; \
	done

fmt:
	gofmt -w $(MODULES)

fmt-check:
	@files="$$(gofmt -l $(MODULES))" || exit 1; \
	if [ -n "$$files" ]; then echo "$$files"; exit 1; fi

tidy:
	GO=$(GO) $(PYTHON) scripts/check-modules.py --tidy

check: fmt-check vet test check-docs

check-modules:
	GO=$(GO) $(PYTHON) scripts/check-modules.py

check-deps:
	$(PYTHON) scripts/check-core-deps.py

check-docs:
	$(PYTHON) scripts/check-doc-links.py
	$(GIT) diff --check

site-install:
	$(PNPM) -C $(SITE) install --frozen-lockfile

site-sync:
	$(PNPM) -C $(SITE) run sync

site-build: site-install
	$(PNPM) -C $(SITE) run build

site-dev: site-install
	$(PNPM) -C $(SITE) run dev

check-site: site-build

release-check:
	goreleaser check

release:
	goreleaser release --clean

verify: check check-modules check-deps check-site

clean:
	rm -rf bin dist $(SITE)/.vitepress/dist

distclean: clean
	rm -rf $(SITE)/node_modules

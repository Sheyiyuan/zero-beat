# zero-beat 仓库根目录 Makefile
#
# 所有目标都从仓库根目录执行；不要在 zero-beat-core/ 或 zero-beat-tui/ 里单独 make。
# 常用入口：
#   make help          查看全部目标
#   make check         格式化 + vet + 两模块测试 + 文档链接
#   make build         构建 bin/zero
#   make verify        本地完整验收（含独立模块消费、平台依赖、站点构建）
#   make site-build    构建 zero-beat-site
#   make distclean     清理构建产物与站点 node_modules

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

.PHONY: all help version build run demo test vet fmt fmt-check tidy \
	check check-modules check-deps check-docs check-site \
	site-install site-sync site-build site-dev \
	clean distclean

all: check build

help:
	@printf '%s\n' \
		'zero-beat 开发入口（仓库根目录）' \
		'' \
		'  make help            显示本帮助' \
		'  make all             check + build（默认）' \
		'  make check           fmt-check + vet + 两模块 test + 文档链接 + git diff --check' \
		'  make build           构建 bin/zero' \
		'  make run             直接运行 TUI（go run）' \
		'  make demo            打印一帧示例界面' \
		'  make test            只跑两模块测试' \
		'  make vet             只跑 go vet' \
		'  make fmt             格式化两模块' \
		'  make fmt-check       检查 gofmt（不修改文件）' \
		'  make tidy            用临时模块代理整理 go.mod/go.sum' \
		'  make check-modules   GOWORK=off 独立消费两个 module' \
		'  make check-deps      四个目标平台的 core 依赖边界与编译' \
		'  make check-docs      检查 Markdown 本地链接与空白错误' \
		'  make site-install    安装站点依赖（pnpm --frozen-lockfile）' \
		'  make site-sync       把 docs/ 同步进站点' \
		'  make site-build      安装依赖并构建站点' \
		'  make site-dev        启动站点开发服务器' \
		'  make verify          check + check-modules + check-deps + check-site' \
		'  make version         打印 zero 版本' \
		'  make clean           删除 bin/ 与站点 dist' \
		'  make distclean       clean + 删除站点 node_modules'

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

verify: check check-modules check-deps check-site

clean:
	rm -rf bin dist $(SITE)/.vitepress/dist

distclean: clean
	rm -rf $(SITE)/node_modules

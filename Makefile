# zero-beat 伞仓 Makefile（仓库根目录）
#
# 子模块：zero-beat-core / zero-beat-tui / zero-beat-gui；网站在主仓库 zero-beat-site。
# 所有目标从本目录执行；TUI 通过 Go module 依赖 core，本地由 go.work 联调。

SHELL := /bin/bash
BINARY := zero
VERSION ?= 0.8.0
GO ?= go
PYTHON ?= python3
PNPM ?= pnpm
GIT ?= git
DART ?= dart
LDFLAGS ?= -s -w -X main.version=$(VERSION)

CORE := zero-beat-core
TUI := zero-beat-tui
GUI := zero-beat-gui
SITE := zero-beat-site
MODULES := $(CORE) $(TUI)

.DEFAULT_GOAL := all

GUI_GO := $(GUI)/rpc $(GUI)/runtime $(GUI)/wire $(GUI)/cmd $(GUI)/tool

.PHONY: all help submodules version build run demo test vet fmt fmt-check tidy \
	check check-modules check-deps check-docs check-site \
	check-gui-bindings check-gui gui-linux gui-android \
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
		'  make check-modules    GOWORK=off 独立消费三个 module' \
		'  make check-deps       四平台 core 依赖边界与编译' \
		'  make check-docs       Markdown 链接 + 空白检查' \
		'  make check-gui-bindings  Go/Dart 类型契约、生成漂移与静态分析（需 Dart）' \
		'  make check-gui        绑定检查 + GUI Go 测试 + RPC Dart 测试 + flutter test' \
		'  make gui-linux        构建 libzero.so 与 Linux debug bundle' \
		'  make gui-android      构建 16KB 对齐的 libzero.so 与 Android debug APK' \
		'  make site-install     安装站点依赖' \
		'  make site-build       读取 TUI/GUI 文档并构建站点' \
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
	gofmt -w $(MODULES) $(GUI_GO)

fmt-check:
	@files="$$(gofmt -l $(MODULES) $(GUI_GO))" || exit 1; \
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

check-gui-bindings:
	$(GO) -C $(GUI) test ./tool/core-bindings/...
	DART="$(DART)" $(GO) -C $(GUI) run ./tool/core-bindings/cmd/generate -check
	cd $(GUI)/packages/zero_beat_core && $(DART) pub get --offline && $(DART) analyze && $(DART) run test/contract_test.dart

check-gui: check-gui-bindings
	$(GO) -C $(GUI) vet ./...
	$(GO) -C $(GUI) test ./...
	cd $(GUI)/packages/zero_beat_rpc && $(DART) analyze && $(DART) test
	bash -c 'source $(GUI)/tool/dev-env.sh && cd $(GUI)/app && flutter analyze --no-pub && flutter test --no-pub'

gui-linux:
	bash -c 'set -eu; source $(GUI)/tool/dev-env.sh; cd $(GUI); GO="$(GO)" ./scripts/build-lib.sh build/libzero.so; cd app; flutter build linux --debug'

gui-android:
	bash -c 'source $(GUI)/tool/dev-env.sh && cd $(GUI) && GO="$(GO)" ./scripts/build-apk.sh'

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

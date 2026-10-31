.PHONY: build build-backend build-frontend test test-backend test-frontend i18n i18n-check

# 一键编译前后端
build: build-backend build-frontend

# 编译后端（复用 backend/Makefile）
build-backend:
	@$(MAKE) -C backend build

# 编译前端（需要已安装依赖）
build-frontend:
	@pnpm --dir frontend run build

# 运行测试（后端 + 前端）
test: test-backend test-frontend

test-backend:
	@$(MAKE) -C backend test

test-frontend: i18n-check
	@pnpm --dir frontend run lint:check
	@pnpm --dir frontend run typecheck
	@pnpm --dir frontend run test:run

# zh-TW 语言包是生成物：改了 locales/zh 之后必须重跑
# （tools/zh-tw 有独立的 node_modules，不碰 frontend 的 pnpm lockfile）
tools/zh-tw/node_modules:
	@cd tools/zh-tw && npm ci

i18n: tools/zh-tw/node_modules
	@node tools/zh-tw/gen-locale.mjs

i18n-check: tools/zh-tw/node_modules
	@node tools/zh-tw/gen-locale.mjs --check

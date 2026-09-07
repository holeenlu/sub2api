.PHONY: build build-backend build-frontend test test-backend test-frontend test-frontend-critical i18n i18n-check

FRONTEND_CRITICAL_VITEST := \
	src/i18n/__tests__/localeKeyCompleteness.spec.ts \
	src/api/__tests__/client.spec.ts \
	src/api/__tests__/tokenRefresh.spec.ts \
	src/api/__tests__/channelMonitorV2.spec.ts \
	src/views/auth/__tests__/LinuxDoCallbackView.spec.ts \
	src/views/auth/__tests__/WechatCallbackView.spec.ts \
	src/views/user/__tests__/PaymentView.spec.ts \
	src/views/user/__tests__/PaymentResultView.spec.ts \
	src/views/user/__tests__/ChannelStatusView.mode.spec.ts \
	src/components/user/profile/__tests__/ProfileInfoCard.spec.ts \
	src/views/admin/__tests__/SettingsView.spec.ts \
	src/features/channel-monitor-v2/__tests__/designSystem.structure.spec.ts \
	src/features/channel-monitor-v2/__tests__/monitorFormat.spec.ts \
	src/features/channel-monitor-v2/__tests__/monitorZoom.spec.ts \
	src/i18n/__tests__/zhTwLocale.spec.ts \
	src/i18n/__tests__/localeKeyCompleteness.spec.ts \
	src/i18n/__tests__/taiwanLocalization.spec.ts \
	src/components/charts/__tests__/TokenUsageTrend.spec.ts \
	src/i18n/__tests__/hardcodedLocaleUsage.spec.ts

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
	@$(MAKE) test-frontend-critical

test-frontend-critical:
	@pnpm --dir frontend exec vitest run $(FRONTEND_CRITICAL_VITEST)

# zh-TW 语言包是生成物：改了 locales/zh 之后必须重跑
# （tools/zh-tw 有独立的 node_modules，不碰 frontend 的 pnpm lockfile）
tools/zh-tw/node_modules:
	@cd tools/zh-tw && npm ci

i18n: tools/zh-tw/node_modules
	@node tools/zh-tw/gen-locale.mjs

i18n-check: tools/zh-tw/node_modules
	@node tools/zh-tw/gen-locale.mjs --check

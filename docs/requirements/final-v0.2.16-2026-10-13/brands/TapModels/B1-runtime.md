# TapModels · 运行品牌与服务身份

## 需求与最终方案

保留来源 87fb1f2613667cb250525af8a4c7c0c12f03c2f6 的最终品牌行为；从已重建 main 继承同一份共享功能与规则。详见本归档 BRAND-TapModels.md。此组仅落以下文件的最终 mode/blob，源码、资源及配置不重新实现。

## 验收边界

完整品牌提交集应与原品牌树逐对象相同，额外仅有本次需求归档与两项共享规则；不能从单个品牌分类提交推断独立部署就绪。没有推送、发布或部署。

## 文件归属

- M backend/internal/config/config.go
- M backend/internal/pkg/logger/options.go
- M backend/internal/pkg/logger/options_test.go
- M backend/internal/pkg/sysutil/restart.go
- M backend/internal/platform/liveattestation/attestation.go
- M backend/internal/server/api_contract_test.go
- M backend/internal/service/brand.go
- M backend/internal/service/data_management_service.go
- M backend/internal/service/update_service.go
- M backend/internal/setup/cli.go
- M backend/internal/setup/setup.go
- M backend/internal/web/embed_on.go
- M backend/internal/web/embed_test.go

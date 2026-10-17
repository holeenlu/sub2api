# tokensavy · 构建、发布、部署和在线更新

## 需求与最终方案

保留来源 f58b507f2c774bb6e6f7f9b8915a84b60a1d3f7e 的最终品牌行为；从已重建 main 继承同一份共享功能与规则。详见本归档 BRAND-tokensavy.md。此组仅落以下文件的最终 mode/blob，源码、资源及配置不重新实现。

## 验收边界

完整品牌提交集应与原品牌树逐对象相同，额外仅有本次需求归档与两项共享规则；不能从单个品牌分类提交推断独立部署就绪。没有推送、发布或部署。

## 文件归属

- M .dockerignore
- M .gitignore
- M .goreleaser.simple.yaml
- M .goreleaser.yaml
- M Dockerfile
- M Dockerfile.goreleaser
- M deploy/.env.example
- M deploy/.gitignore
- M deploy/APPLE_CONTAINER.md
- M deploy/Caddyfile
- M deploy/DATAMANAGEMENTD_CN.md
- M deploy/DOCKER.md
- M deploy/Dockerfile
- M deploy/EDGE_SECURITY.md
- M deploy/README.md
- M deploy/apple-container.sh
- M deploy/build_image.sh
- M deploy/config.example.yaml
- M deploy/docker-compose.dev.yml
- M deploy/docker-compose.local.yml
- M deploy/docker-compose.standalone.yml
- M deploy/docker-compose.wsl.yml
- M deploy/docker-compose.yml
- M deploy/docker-deploy.sh
- M deploy/docker-entrypoint.sh
- M deploy/install-datamanagementd.sh
- M deploy/install.sh
- D deploy/kdan-datamanagementd.service
- D deploy/kdan.service
- M deploy/tests/apple-container-test.sh
- M deploy/tests/docker-compose-gateway-env-test.sh
- M deploy/tests/docker-compose-security-test.sh
- M deploy/tests/docker-runtime-resources-test.sh
- M deploy/tests/fixtures/bin/container
- M deploy/tests/install-github-token-test.sh
- A deploy/tests/test_tokensavy.py
- A deploy/tokensavy-datamanagementd.service
- A deploy/tokensavy.service
- A deploy/tokensavy/.env.example
- A deploy/tokensavy/Caddyfile
- A deploy/tokensavy/README.md
- A deploy/tokensavy/compose.source.yaml
- A deploy/tokensavy/compose.yaml
- A deploy/tokensavy/init.py

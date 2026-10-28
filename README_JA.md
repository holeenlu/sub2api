# sub2api

AI APIゲートウェイの独立した再構築プロジェクトです。開発ブランチはNewAPIです。既存のmainおよびブランド別ブランチは別に保守します。

現在、バックエンドは単独でビルド・起動できます。実行エンジン全体と他のドメインは移行中です。フロントエンドは別の開発セッションが担当し、標準Dockerイメージには含まれません。

~~~sh
docker build -t sub2api:local .
make build-backend
~~~

- [APIデプロイ手順](deploy/api/README.md)
- [設計と開発計画](docs/architecture-rebuild/README.md)
- [実装と検証結果](docs/architecture-rebuild/IMPLEMENTATION.md)
- [フロントエンドとの引き継ぎ](docs/architecture-rebuild/FRONTEND_HANDOFF.md)
- [ライセンス](LICENSE)と[出典](docs/SOURCE_ADAPTATIONS.md)

サイト名、Logo、公開APIリンクは実行時設定、イメージ名とコンテナ名はデプロイ設定で管理します。旧ブランド向けの配布物や自動更新インストーラーは使用しません。更新はこのブランチからビルドした成果物を再デプロイして行います。

[English](README.md) · [中文](README_CN.md)

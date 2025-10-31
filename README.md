# infra-practice

## Media API

Goを使った写真保存API

### サーバー起動

```bash
cd media-api
go mod tidy
go run cmd/main.go
```

サーバーは http://localhost:8080 で起動します

### API仕様

- `GET /healthz` - ヘルスチェック（認証不要）
- `POST /api/v1/media` - 写真アップロード
- `GET /api/v1/media` - 写真一覧
- `GET /api/v1/media/{id}` - 写真詳細
- `GET /api/v1/media/{id}/file` - 写真ファイル取得

認証が必要なAPIは Authorization ヘッダーに `Bearer <token>` を設定

### テスト実行

```bash
cd media-api

# 単体テスト
go test -v ./tests/unit/

# 統合テスト  
go test -v ./tests/integration/

# 全テスト
go test -v ./tests/...
```

### 使用例

```bash

# 写真一覧取得
curl -H "Authorization: Bearer your-super-secret-token-here" \
     http://localhost:8080/api/v1/media

# 写真アップロード
curl -X POST \
     -H "Authorization: Bearer your-super-secret-token-here" \
     -F "file=@image.jpg" \
     http://localhost:8080/api/v1/media
```

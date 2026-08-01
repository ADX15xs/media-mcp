# media-mcp

MCP (Model Context Protocol) 服务，提供媒体生成能力，支持多种 AI 供应商。

## Project

- **语言**: Go 1.26.3
- **入口点**: `cmd/media-mcp/main.go`
- **配置**: `config.yml` (YAML 格式)
- **依赖**: `gopkg.in/yaml.v3`

## Commands

```bash
# 构建
make build          # 输出到 build/media-mcp
make windows        # Windows amd64
make linux          # Linux amd64
make darwin         # macOS amd64
make all            # 全平台构建
make clean          # 清理 build 目录

# 运行
make run            # go run cmd/media-mcp
go run cmd/media-mcp --config config.yml

# 测试
make test           # go test -v ./...
```

## Architecture

- **`cmd/media-mcp/main.go`** — 主入口，解析配置并注册供应商
- **`internal/config/config.go`** — 配置加载，支持环境变量 `MEDIA_MCP_CONFIG`
- **`internal/mcp/transport.go`** — MCP server 实现，stdio 传输
- **`internal/supplier/`** — 供应商适配器层
  - `registry.go` — 供应商注册表
  - `supplier.go` — 供应商接口定义
  - `agnes_ai.go` — Agnes AI 适配器
  - `doubao_seedream.go` — 豆包 Seedream 适配器
  - `sensenova.go` — SenseNova 适配器
  - `http_generic.go` — 通用 HTTP 适配器（支持自定义供应商）
- **`images-generations/`** — 各供应商 API 文档参考

## Conventions

- 供应商通过 `config.yml` 动态注册，支持 `image`/`video`/`both` 类型
- 通用 HTTP 适配器支持任意 OpenAI 兼容 API
- 错误处理使用标准 Go error 模式
- 日志输出到 stderr，工具结果输出到 stdout

## Notes

<!-- 后续补充项目特定信息 -->

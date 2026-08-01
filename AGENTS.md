# media-mcp

MCP (Model Context Protocol) 服务器，将多种 AI 媒体生成 API 统一暴露为 MCP 工具，供 Agent 调用。

## Project

- **语言**: Go 1.26.3
- **唯一依赖**: `gopkg.in/yaml.v3`
- **入口**: `cmd/media-mcp/main.go`
- **配置**: `config.yml`（YAML，支持 `${ENV_VAR}` 展开），可通过 `MEDIA_MCP_CONFIG` 环境变量指定
- **MCP 协议**: stdio JSON-RPC，协议版本 `2024-11-05`，服务器名 `media-mcp-server` v0.2.0
- **受忽略文件**: `.env`, `config.yml`, `build/`

## Commands

```bash
make build          # go build → build/media-mcp
make run            # go run ./cmd/media-mcp
make test           # go test -v ./... （当前无测试文件）
make windows        # GOOS=windows GOARCH=amd64
make linux          # GOOS=linux GOARCH=amd64
make darwin         # GOOS=darwin GOARCH=amd64
make all            # clean + build + 全平台
make clean          # rm -rf build/
```

## Architecture

```
cmd/media-mcp/main.go
  ↓ 加载 config.yml
internal/config/config.go          ← SupplierConfig / GlobalConfig，环境变量展开
  ↓
internal/mcp/transport.go          ← MCP stdio 服务，动态注册工具
  ↓
internal/supplier/
  supplier.go                      ← ImageSupplier / VideoSupplier 接口 + 请求/结果类型
  registry.go                      ← 按名称工厂注册（本项目 main.go 直接 switch）
  sensenova.go                     ← 商汤 SenseNova（同步）
  agnes_ai.go                      ← Agnes AI 图像（2.0/2.1 Flash，支持 ratios）
  agnes_video.go                   ← Agnes AI 视频（异步任务 + 轮询）
  doubao_seedream.go               ← 豆包 Seedream（火山方舟 Agent Plan）
  http_generic.go                  ← OpenAI 兼容通用适配器（兜底）
images-generations/                ← 各供应商 API 文档参考（Agnes.md 等）
```

## Conventions

- **工具命名**: `{supplier}_generateImage` / `{supplier}_generateVideo`，由 MCP server 根据启用供应商动态生成
- **认证方式**: `config.yml` 中 `auth_method` 支持 `bearer`（默认）、`basic`、`custom_header`
- **环境变量展开**: 配置值中的 `${VAR}` 在启动时展开；缺失变量保留原值，运行时报错
- **供应商注册**: main.go 用显式 `switch` 按 name 创建对应适配器（非 registry 包），新增供应商需改 main.go
- **视频生成**: 异步模式，提交 task_id 后每 5s 轮询，最长等 10 分钟
- **错误处理**: 标准 Go error，日志到 stderr，JSON-RPC 响应到 stdout
- **无测试**: 目前无任何 `_test.go` 文件

## Notes

<!-- 后续补充项目特定信息 -->

# media-mcp

MCP (Model Context Protocol) 服务器，将多种 AI 媒体生成 API 统一暴露为 MCP 工具，供 Agent 调用。

## Project

- **语言**: Go 1.25.0
- **唯一依赖**: `gopkg.in/yaml.v3`
- **入口**: `cmd/media-mcp/main.go`
- **配置**: `config.yml`（YAML，支持 `${ENV_VAR}` 展开），可通过 `MEDIA_MCP_CONFIG` 环境变量指定
- **MCP 协议**: stdio JSON-RPC，协议版本 `2024-11-05`，服务器名 `media-mcp-server` v0.2.0
- **受忽略文件**: `.env`, `config.yml`, `build/`

## Commands

```bash
make build          # go build → build/media-mcp
make run            # go run ./cmd/media-mcp
make test           # go test -v ./...
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
  registry.go                      ← 注册表工厂（图像/视频 builders）
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
- **供应商注册**: 适配器通过 `init()` 自注册到 `registry` 包；`main.go` 调用 `supplier.BuildAll(cfg)` 统一构建，未注册的供应商名自动 fallback 到 `HTTPGenericAdapter` / `HTTPGenericVideoAdapter`
- **provider 专属参数声明（`SchemaExtender`）**: 工具基础 schema 只含通用字段（图像 `prompt/model/size/n`；视频 `prompt/model/duration/style/seed/aspect_ratio/resolution`）。provider 专属参数（如 agnes 图像 `ratio`/图生图、doubao `negative_prompt`/`max_images` 等、agnes_video 重连 `task_id`/`video_id`）由各 adapter 实现 `SchemaExtender.ExtraInputSchema()` 声明：transport 合并进该工具 schema，并把匹配的调用参数转发进 `req.Extra`。未声明的参数不可达、也不会外泄到其他 provider（含通用兜底）。基础字段优先生效，provider 不可覆盖
- **视频生成**: 异步模式。提交后先等 15s，再自适应轮询（5–30s），总超时 15 分钟。**创建接口限流 1 请求/分钟**（Agnes 实测），多任务必须串行提交且间隔 ≥ 60s
- **视频尺寸控制**: `aspect_ratio`（9:16/16:9/1:1/4:3/3:4）+ `resolution`（480p/720p/1080p）是 `VideoRequest` 的**第一等可选字段**（非 `Extra`），对所有视频 provider 通用；各 adapter 自行决定是否映射为 width/height。查表+校验逻辑在共享的 `supplier.SizeTable.ResolveSize`（数据由 provider 自持，无硬编码像素值，可复用无技术债）；agnes 的默认 16:9/720p 仅当至少传一个字段时生效，非法值显式报错。注意 agnes 上游对 width/height 会归一化（1152×768 → 实际 1088×832），像素不精确保证（探针校准后更新）
- **视频轮询容错**: Agnes 状态接口会间歇性返回 404 / 429，但任务在服务端仍会跑完。所有 HTTP 失败一律视为**瞬时错误**，在双端点间回退重试；只有全部端点在 3 分钟宽限期内持续失败才放弃任务，且放弃时必定打印 `task_id` / `video_id` 与找回命令
- **视频任务重连**: 仅 `agnes_video_generateVideo`（经 `SchemaExtender` 声明）支持可选的 `task_id` / `video_id` 参数，跳过创建直接挂到已有任务上取回结果，不重复计费
- **调试日志**: `MEDIA_MCP_DEBUG=1` 将完整请求/响应体输出到 stderr
- **错误处理**: 标准 Go error，日志到 stderr，JSON-RPC 响应到 stdout
- **测试**: 各包目录下的 `_test.go` 文件，遵循标准 Go 测试约定

## Notes

<!-- 后续补充项目特定信息 -->

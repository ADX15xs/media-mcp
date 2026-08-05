# Media MCP Server

多供应商生图 API 的统一 MCP（Model Context Protocol）服务器。支持 SenseNova、Agnes AI、Doubao Seedream 三大平台。

## 架构

```
┌──────────────┐     MCP Protocol      ┌─────────────────────┐
│   Agent A    │◄─────────────────────►│                     │
│   Agent B    │                       │  Unified MCP Server  │
│   Agent C    │                       │  (Go binary)         │
└──────────────┘                       └─────────┬───────────┘
                                                 │
                                    config.yml 动态加载
                                    ┌──────────┼──────────┬──────────┐
                                    ▼          ▼          ▼          ▼
                               senseNova   agnes_ai   doubao     [generic]
```

## 已支持的供应商

| 供应商 | 配置 key | 模型 | 端点 | 特殊要求 |
|--------|---------|------|------|---------|
| **SenseNova** | `senseNova` | sensenova-u1-fast | token.sensenova.cn | base64 自动保存为临时文件 |
| **Agnes AI** | `agnes_ai` | agnes-image-2.0-flash / 2.1-flash | api.agnes-ai.cn | `response_format` 需放在 `extra_body` 内 |
| **Doubao Seedream** | `doubao_seedream` | 5.0-lite / 5.0-pro / 4.5 / 4.0 | ark.cn-beijing.volces.com | 支持 `output_format`、`watermark`、组图生成、联网搜索 |
| **Agnes Video** | `agnes_video` | agnes-video-v2.0 | api.agnes-ai.cn | 异步任务，自动轮询，返回 video_url |

## 视频生成支持

视频生成为异步任务模式，支持以下流程：

1. **提交任务** — 调用视频生成 API，获得 `task_id`
2. **自动轮询** — 每 5 秒查询一次任务状态，最长等待 10 分钟
3. **返回结果** — 任务完成后返回视频 URL

工具名称：`{supplier}_generateVideo`

调用示例：
```jsonc
{
  "name": "agnes_video_generateVideo",
  "arguments": {
    "prompt": "一只白色狐狸在月光下的雪原中奔跑，电影质感",
    "model": "agnes-video-v2.0",
    "duration": 5,
    "style": "cinematic"
  }
}
```

启用视频供应商需配置 `config.yml`：
```yaml
agnes_video:
  enabled: true
  type: video
  api_key: ${AGNES_AI_API_KEY}
  base_url: https://api.agnes-ai.cn/v1/videos
  model: agnes-video-v2.0
  extra:
    width: 1152
    height: 768
    num_frames: 121      # 约 5 秒 (121/24 ≈ 5s)
    frame_rate: 24
```

## 快速开始

### 1. 安装依赖

```bash
go mod tidy
```

### 2. 配置

```bash
cp config.yml.example config.yml
```

编辑 `config.yml`，填入你自己的 API Key：

```yaml
suppliers:
  senseNova:
    enabled: true
    api_key: ${SENSENOVA_API_KEY}    # 会被自动替换为环境变量值
    base_url: https://token.sensenova.cn/v1/images/generations
    model: sensenova-u1-fast
    size: 2752x1536
```

### 3. 设置环境变量

```bash
export SENSENOVA_API_KEY="sk-your-actual-key"
# 或写入 .env 文件并 source
```

### 4. 运行

```bash
# 开发模式
go run ./cmd/media-mcp/

# 构建
make build

# 运行
./media-mcp

# 指定自定义配置路径
./media-mcp --config /path/to/config.yml
```

## 5. 接入 MCP 客户端

MCP 客户端以子进程方式启动服务器。对 `command`（二进制）和 `--config` 参数都**使用绝对路径**——
相对路径会基于客户端自身的工作目录解析，而那通常并不是你的目录。

### Reasonix (`config.toml`, `reasonix.toml`)

```toml
[[plugins]]
name    = "media-mcp"
type    = "stdio"
command = "C:\\absolute\\path\\to\\media-mcp.exe"
args    = ["--config", "C:\\absolute\\path\\to\\config.yml"]
env     = { AGNES_AI_API_KEY = "${AGNES_AI_API_KEY}", SENSENOVA_API_KEY = "${SENSENOVA_API_KEY}", VOLC_API_KEY = "${VOLC_API_KEY}" }
```

### Claude Desktop（`claude_desktop_config.json`）

```json
{
  "mcpServers": {
    "media-mcp": {
      "command": "/absolute/path/to/media-mcp",
      "args": ["--config", "/absolute/path/to/config.yml"],
      "env": {
        "AGNES_AI_API_KEY": "sk-your-agnes-key",
        "SENSENOVA_API_KEY": "sk-your-sensenova-key",
        "VOLC_API_KEY": "sk-your-volc-key"
      }
    }
  }
}
```

### VS Code（`settings.json` → `mcp.servers`）

```json
{
  "mcp": {
    "servers": {
      "media-mcp": {
        "command": "C:\\absolute\\path\\to\\media-mcp.exe",
        "args": ["--config", "C:\\absolute\\path\\to\\config.yml"],
        "env": {
          "AGNES_AI_API_KEY": "sk-your-agnes-key",
          "SENSENOVA_API_KEY": "sk-your-sensenova-key",
          "VOLC_API_KEY": "sk-your-volc-key"
        }
      }
    }
  }
}
```

## 工具列表

启动后，Agent 可以通过 `tools/list` 自动发现可用的工具：

- `senseNova_generateImage` — 使用 SenseNova 生成图片
- `agnes_ai_generateImage` — 使用 Agnes AI 生成图片（需启用）
- `agnes_video_generateVideo` — 使用 Agnes AI 生成视频（需启用）
- `{supplier}_generateImage` — 任何已启用的图像供应商都会暴露为独立 tool
- `{supplier}_generateVideo` — 任何已启用的视频供应商都会暴露为独立 tool

### 调用示例

```jsonc
// MCP tools/call
{
  "name": "senseNova_generateImage",
  "arguments": {
    "prompt": "A cute cat sitting on a cloud",
    "model": "sensenova-u1-fast",
    "size": "1024x1024",
    "n": 1
  }
}
```

## 新增供应商

只需两步：

1. **写 Adapter**（约 50-80 行 Go 代码）— 实现 `ImageSupplier` 接口，并通过 `init()` 调用 `supplier.RegisterImage(name, fn)` 自注册
2. **在 config.yml 里加一段配置** — 启用它

> `main.go` 调用 `supplier.BuildAll(cfg)` 统一构建所有启用的供应商，无需手动改 `main.go`。未注册的供应商名会自动 fallback 到下方的 Generic HTTP 适配器。

### Generic HTTP 适配器

对于 OpenAI 兼容格式的供应商（如大多数第三方生图平台），无需写代码，直接加配置即可被 `BuildAll` 自动识别：

```yaml
suppliers:
  my_supplier:
    enabled: true
    type: image
    api_key: ${MY_API_KEY}
    base_url: https://api.example.com/v1/images/generations
    model: my-model-v1
    size: "1024x1024"
    auth_method: bearer    # options: bearer, basic, custom_header
```

`my_supplier` 这个名字未在 registry 中注册，`BuildAll` 会自动为其创建 `HTTPGenericAdapter`（图像）或 `HTTPGenericVideoAdapter`（视频），无需任何额外代码。

## 配置变量展开

`${VAR_NAME}` 在启动时自动替换为对应环境变量：

```yaml
api_key: ${SENSENOVA_API_KEY}   → 实际值取自 $SENSENOVA_API_KEY
base_url: ${BASE_URL:-https://default.url}  # 支持默认值语法
```

## 目录结构

```
media-mcp/
├── cmd/media-mcp/main.go       # CLI 入口
├── internal/
│   ├── mcp/
│   │   └── transport.go        # MCP JSON-RPC server (stdio)
│   ├── config/
│   │   └── config.go           # YAML 配置加载 + 环境变量展开
│   └── supplier/
│       ├── supplier.go         # ImageSupplier / VideoSupplier 接口
│       ├── registry.go         # 工厂注册表
│       ├── sensenova.go        # SenseNova 专用适配器
│       ├── agnes_ai.go         # Agnes AI 图像适配器
│       ├── agnes_video.go      # Agnes AI 视频适配器（异步轮询）
│       ├── doubao_seedream.go  # Doubao Seedream 适配器
│       └── http_generic.go     # 通用 HTTP 适配器（用于第三方）
├── config.yml                  # 每机独立（不进 git）
├── config.yml.example          # 模板（进 git）
├── go.mod / go.sum
├── Makefile
└── .gitignore
```

## License

MIT

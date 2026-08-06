# Agnes AI

Agnes AI 提供的图像和视频生成模型，包含三个子模型：

| 模型 | Model ID | 类型 |
| --- | --- | --- |
| Agnes Image 2.1 Flash | `agnes-image-2.1-flash` | 文生图、图生图（高信息密度优化） |
| Agnes Video V2.0 | `agnes-video-v2.0` | 文生视频、图生视频、关键帧动画（异步） |

> Base URL: `https://api.agnes-ai.cn`

---

## Agnes Image 2.0 Flash

### 请求示例（文生图）

```bash
curl https://api.agnes-ai.cn/v1/images/generations \
  -H "Authorization: Bearer $AGNES_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "agnes-image-2.1-flash",
    "prompt": "A clean product photo of a glass cube on a white studio background, soft shadows, high detail",
    "size": "1024x768",
    "extra_body": {
      "response_format": "url"
    }
  }'
```

### 请求参数

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | ✅ | 固定为 `agnes-image-2.1-flash` |
| `prompt` | string | ✅ | 图像描述文本 |
| `size` | string | ✅ | 输出尺寸，如 `1024x768`、`1024x1024`、`768x1024` |
| `image` | string[] | 图生图必填 | 输入图像数组，支持公网 URL 或 Data URI Base64 |
| `return_base64` | boolean | — | 文生图需要返回 Base64 时使用 |
| `extra_body.response_format` | string | — | 输出格式：`url` 或 `b64_json` |

### 重要说明

- `response_format` 必须放在 `extra_body` 内部，不能在顶层
- 图生图不需要传递 `tags: ["img2img"]`
- 客户端超时建议设置为 `60s - 360s`

---

## Agnes Image 2.1 Flash

### 请求示例（文生图）

```bash
curl https://api.agnes-ai.cn/v1/images/generations \
  -H "Authorization: Bearer $AGNES_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "agnes-image-2.1-flash",
    "prompt": "A luminous floating city above a misty canyon at sunrise, cinematic realism",
    "size": "2K",
    "ratio": "16:9",
    "extra_body": {
      "response_format": "url"
    }
  }'
```

### 请求参数

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | ✅ | 固定为 `agnes-image-2.1-flash` |
| `prompt` | string | ✅ | 图像描述文本 |
| `size` | string | ✅ | 尺寸档位：`1K`、`2K`、`3K`、`4K`，或精确尺寸如 `1024x768` |
| `ratio` | string | — | 宽高比：`1:1`、`3:4`、`4:3`、`16:9`、`9:16`、`2:3`、`3:2`、`21:9`，默认 `1:1` |
| `image` | string[] | 图生图必填 | 输入图像数组，支持公网 URL 或 Data URI Base64 |
| `return_base64` | boolean | — | 文生图需要返回 Base64 时使用 |
| `extra_body.response_format` | string | — | 输出格式：`url` 或 `b64_json` |

### 尺寸参考

| Ratio | 1K | 2K | 3K | 4K |
| --- | --- | --- | --- | --- |
| `1:1` | `1024x1024` | `2048x2048` | `3072x3072` | `4096x4096` |
| `16:9` | `1312x736` | `2624x1472` | `3936x2208` | `5248x2944` |
| `9:16` | `736x1312` | `1472x2624` | `2208x3936` | `2944x5248` |
| `4:3` | `1152x864` | `2304x1728` | `3456x2592` | `4608x3456` |
| `3:4` | `864x1152` | `1728x2304` | `2592x3456` | `3456x4608` |

### 重要说明

- 建议使用 `size` + `ratio` 组合获得可预期的输出尺寸
- `response_format` 必须放在 `extra_body` 内部
- 图生图不需要传递 `tags: ["img2img"]`

---

## Agnes Video V2.0

### 创建任务示例（文生视频）

```bash
curl -X POST https://api.agnes-ai.cn/v1/videos \
  -H "Authorization: Bearer $AGNES_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "agnes-video-v2.0",
    "prompt": "A cinematic shot of a cat walking on the beach at sunset, soft ocean waves, warm golden lighting, realistic motion",
    "height": 768,
    "width": 1152,
    "num_frames": 121,
    "frame_rate": 24
  }'
```

### 创建任务参数

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | ✅ | 固定为 `agnes-video-v2.0` |
| `prompt` | string | ✅ | 视频内容描述 |
| `image` | string | — | 图生视频用的图片 URL |
| `mode` | string | — | 生成模式：`ti2vid`（文生视频）、`keyframes`（关键帧动画） |
| `width` | integer | — | 视频宽度，默认 `1152` |
| `height` | integer | — | 视频高度，默认 `768` |
| `num_frames` | integer | — | 视频帧数，必须 `≤ 441` 且遵循 `8n + 1` 规则 |
| `frame_rate` | number | — | 帧率，支持 `1–60` |
| `extra_body.image` | array | — | 关键帧动画的输入图片 URL 数组 |
| `extra_body.mode` | string | — | 附加模式设置，如 `keyframes` |

### 获取结果

```bash
# 推荐方式
curl https://api.agnes-ai.cn/agnesapi?video_id=<VIDEO_ID> \
  -H "Authorization: Bearer $AGNES_API_KEY"

# 兼容旧版
curl https://api.agnes-ai.cn/v1/videos/<TASK_ID> \
  -H "Authorization: Bearer $AGNES_API_KEY"
```

### 任务状态

| 状态 | 说明 |
| --- | --- |
| `queued` | 等待中 |
| `in_progress` | 生成中 |
| `completed` | 完成 |
| `failed` | 失败 |

### 视频时长控制

```
seconds = num_frames / frame_rate
```

| 目标时长 | 推荐参数 |
| --- | --- |
| 约 3 秒 | `num_frames: 81`, `frame_rate: 24` |
| 约 5 秒 | `num_frames: 121`, `frame_rate: 24` |
| 约 10 秒 | `num_frames: 241`, `frame_rate: 24` |
| 约 18 秒 | `num_frames: 441`, `frame_rate: 24` |

> MCP 工具的 `duration`（秒）参数会在适配器内自动换算为 `num_frames`
> （取最近的 8n+1 值，上限 441），无需手动指定 `num_frames`。

### 支持分辨率档位

| 档位 | 说明 |
| --- | --- |
| `480p` | 标准清晰度 |
| `720p` | 高清 |
| `1080p` | 全高清 |

支持宽高比：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`

### 两个查询端点的差异（实测）

| 端点 | 状态字段 | 是否含视频 URL | 备注 |
| --- | --- | --- | --- |
| `agnesapi?video_id=` | `status` / `internal_status` | ✅ 顶层 `url` | **唯一**能拿到成品 URL 的端点，进度更新更快 |
| `/v1/videos/<TASK_ID>` | `status` | ❌ 从不返回 URL | 但会返回 `video_id`，可据此转查 agnesapi；状态更新有滞后 |
| `/v1/videos/<TASK_ID>/content` | — | ❌ | 恒返回 `{"detail":"Not Found"}`（HTTP 200 + `video/mp4` 头），不可用 |

### 限流与瞬时错误（重要）

实测结论：

- **创建接口限流：1 请求/分钟**。错误消息：`video generation rate limit exceeded: allows 1 requests per 1 minute(s)`。连续提交多个任务时第 2 个起直接 429，任务**必须串行提交且间隔 ≥ 60s**（2026-08-06 实测确认）。
- **状态接口限流更激进**：4 路并发轮询时约 **29%** 的请求返回 `HTTP 429 video status query rate limit exceeded`；1s 间隔单路轮询也会零星命中。
- **状态接口会间歇性返回 `HTTP 404 {"error":{"code":404,"message":"task not found"}}`**，但任务在服务端仍会正常跑完并出现在后台记录里。

因此 **404 / 429 都必须当作瞬时错误重试**，绝不能当作任务失败直接退出，否则会丢弃一个实际已成功的任务（且已计费）。适配器的做法：双端点回退 + 3 分钟宽限期 + 放弃时打印 `task_id` / `video_id`。

### 结果找回

任务丢失时用 `task_id` 找回：

```bash
# 1) 用 task_id 换 video_id
curl -H "Authorization: Bearer $AGNES_API_KEY" https://api.agnes-ai.cn/v1/videos/<TASK_ID>

# 2) 用 video_id 取 url
curl -H "Authorization: Bearer $AGNES_API_KEY" "https://api.agnes-ai.cn/agnesapi?video_id=<VIDEO_ID>"
```

或直接调用 MCP 工具 `agnes_video_generateVideo` 并传入 `task_id`，跳过创建直接取回结果。

### 重要说明

- 视频生成是异步任务，需先创建任务再轮询结果
- 创建任务响应返回 `video_id`（推荐）和 `task_id`
- 最终视频 URL 位于 `agnesapi` 响应的顶层 `url` 字段
- `seconds` 是**字符串**（如 `"5.0"`），不是数字；`frame_rate` 在 `request_params` 内，顶层没有
- `video_id` 是 base64 编码的 LiteLLM 路由令牌，其中的 `model_id` 在任务完成后会被重写为空值，属正常现象
- `num_frames` 必须遵循 `8n + 1` 规则且 `≤ 441`

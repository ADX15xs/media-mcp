# media-mcp 项目长期记忆

## 架构约定

- **视频尺寸控制是 `VideoRequest` 第一等字段**（`AspectRatio`/`Resolution`，string），不是 `Extra` 走私字段。所有视频 provider 通用。
- **共享映射逻辑放 `internal/supplier/size.go`**：`SizeTable` 类型 + `ResolveSize(ar, res)` 只做查表+校验，由 provider 传入自有像素表；不含硬编码像素值或默认策略 → 新增 provider 零技术债。各 adapter 自持 `videoSizeTable` 并保留自己的默认策略（如 agnes 的 16:9/720p 仅当至少传一个字段时生效）。
- **provider 专属参数一律经 `SchemaExtender.ExtraInputSchema()` 声明**（`supplier.go` 可选接口）：transport 把声明合并进该工具 schema，并把匹配参数转发进 `req.Extra`；基础字段优先，provider 不可覆盖。transport 不再硬编码任何 provider 字段名（图像基础字段 `prompt/model/size/n`；视频 `prompt/model/duration/style/seed/aspect_ratio/resolution`）。
- **`Extra` 只承载 SchemaExtender 声明的 provider 专属字段**；未声明的参数不可达、也不外泄（含通用兜底路径）。
- **视频通用性路线**：未注册 provider 自动 fallback 到 `HTTPGenericAdapter`/`HTTPGenericVideoAdapter`（零代码接入）。跨 provider 通用概念应做成第一等字段（如尺寸），单 provider 能力走 SchemaExtender。
- 现有实现 SchemaExtender 的 adapter：`AgnesAIAdapter`（ratio/image/return_base64）、`DoubaoSeedreamAdapter`（negative_prompt/image/max_images/stream/optimize_mode[enum standard/fast]/web_search）。`AgnesVideoAdapter` 已不再实现 SchemaExtender（task_id/video_id 移到 `getVideoResult` 工具 schema，由 transport 硬编码）。
- **工具能力约束（`CapabilityProvider`）**：实现者把关键约束追加到工具描述——agnes_video（限流 1req/min 须串行、时长上限 ~18s、32 对齐、**video_id 为推荐轮询键**）、doubao（size 档位按模型：5.0 lite 为 2K/3K/4K、或 WxH 像素，两种都支持不可混用）、agnes_ai（size 归一化）。**供应商偏好只进各自 Capabilities，transport 通用 schema/描述保持中性**（P2 教训：抽象层不写供应商偏向）。`getVideoResult` schema 用 `anyOf` 约束 task_id/video_id 至少传一个。
- **未识别参数提示**：transport 计算 `req.UnknownArgs`（非基础字段且非 SchemaExtender 声明字段），在结果文本追加 `Note: unexpected argument(s) ignored: ...`；正常调用零开销、无输出。
- **视频非阻塞两段式（2026-08-07 重构）**：`generateVideo` 仅提交、立即返 `Status:"working"` + `task_id`/`video_id`，不阻塞轮询（根治客户端 `-32001`）。完成态由 `{supplier}_getVideoResult` 轮询：可选接口 `VideoStatusProvider{ GetVideoResult(taskID, videoID) }`（`supplier.go`），transport 仅对实现者注册 `getVideoResult` 工具并分发。`pollTask` 有界轮询 `statusPollCap`（20s 常量），cap 耗尽返 `working`（不报错、不放弃）；已删除旧的 15min `totalTimeout` + 3min `grace` + `abandon` 机制与 `initialWait` 前置睡眠（全为死代码）。agnes 与 http_generic_video 均实现 `VideoStatusProvider`。
- **图像 HTTP 超时共享常量** `imageHTTPTimeout`（50s，`supplier.go`）：所有图像 adapter（agnes_ai/doubao/sensenova/http_generic）统一引用，确保在客户端 ~60s 超时前干净报错；`http_generic.go` 原本 `http.Client{}` 无超时（会无限挂）已修复。

## 已知技术债

- 无重大遗留。图像侧 NegativePrompt 死字段、能力困在 Extra 不可达、缺 schema 扩展钩子——已全部由 SchemaExtender 方案解决（2026-08-06）。视频阻塞式 `tools/call` 导致 `-32001`——已由非阻塞两段式重构解决（2026-08-07）。

## 关键事实

- Agnes 视频上游**不精确遵循 width/height，但精确保留比例**，输出均 32 对齐（720→704、1080→1088、854→832 等）。详见 `images-generations/Agnes.md` 与探针结论。
- Agnes 创建接口限流 **1 请求/分钟**，多任务须串行且间隔 ≥60s。
- Agnes 返回的 `video_id` 是 **LiteLLM 复合句柄**：base64(`litellm:custom_llm_provider:openai;model_id:...;video_id:video_...`)，把真实 video_id 与 provider/model 元数据打包（Agnes 网关由 LiteLLM 前端、openai shim 路由）。media-mcp 按**不透明句柄**处理，仅透传/回显，不解析内容；状态查询经 `agnesapi?video_id=<原样>` 走，`url.QueryEscape` 保证 base64 字符安全。
- `negative_prompt` 仅 doubao 图像支持（agnes/sensenova 文档未提及），所以它在基础 schema 之外、由 doubao 经 SchemaExtender 声明。

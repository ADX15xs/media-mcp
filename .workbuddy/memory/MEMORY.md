# media-mcp 项目长期记忆

## 架构约定

- **视频尺寸控制是 `VideoRequest` 第一等字段**（`AspectRatio`/`Resolution`，string），不是 `Extra` 走私字段。所有视频 provider 通用。
- **共享映射逻辑放 `internal/supplier/size.go`**：`SizeTable` 类型 + `ResolveSize(ar, res)` 只做查表+校验，由 provider 传入自有像素表；不含硬编码像素值或默认策略 → 新增 provider 零技术债。各 adapter 自持 `videoSizeTable` 并保留自己的默认策略（如 agnes 的 16:9/720p 仅当至少传一个字段时生效）。
- **provider 专属参数一律经 `SchemaExtender.ExtraInputSchema()` 声明**（`supplier.go` 可选接口）：transport 把声明合并进该工具 schema，并把匹配参数转发进 `req.Extra`；基础字段优先，provider 不可覆盖。transport 不再硬编码任何 provider 字段名（图像基础字段 `prompt/model/size/n`；视频 `prompt/model/duration/style/seed/aspect_ratio/resolution`）。
- **`Extra` 只承载 SchemaExtender 声明的 provider 专属字段**；未声明的参数不可达、也不外泄（含通用兜底路径）。
- **视频通用性路线**：未注册 provider 自动 fallback 到 `HTTPGenericAdapter`/`HTTPGenericVideoAdapter`（零代码接入）。跨 provider 通用概念应做成第一等字段（如尺寸），单 provider 能力走 SchemaExtender。
- 现有实现 SchemaExtender 的 adapter：`AgnesAIAdapter`（ratio/image/response_format/return_base64）、`DoubaoSeedreamAdapter`（negative_prompt/image/max_images/stream/optimize_mode/web_search）、`AgnesVideoAdapter`（task_id/video_id 重连）。

## 已知技术债

- 无重大遗留。图像侧 NegativePrompt 死字段、能力困在 Extra 不可达、缺 schema 扩展钩子——已全部由 SchemaExtender 方案解决（2026-08-06）。

## 关键事实

- Agnes 视频上游**不精确遵循 width/height，但精确保留比例**，输出均 32 对齐（720→704、1080→1088、854→832 等）。详见 `images-generations/Agnes.md` 与探针结论。
- Agnes 创建接口限流 **1 请求/分钟**，多任务须串行且间隔 ≥60s。
- `negative_prompt` 仅 doubao 图像支持（agnes/sensenova 文档未提及），所以它在基础 schema 之外、由 doubao 经 SchemaExtender 声明。

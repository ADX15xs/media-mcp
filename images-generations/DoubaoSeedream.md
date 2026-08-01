# Doubao Seedream 5.0 Lite

基于豆包 Seedream 5.0 Lite 的图片生成模型。

> model ID: doubao-seedream-5.0-lite
> Base URL: `https://ark.cn-beijing.volces.com/api/plan/v3`

## 请求示例

```bash
curl https://ark.cn-beijing.volces.com/api/plan/v3/images/generations \
  -H "Authorization: Bearer $AGENT_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedream-5.0-lite",
    "prompt": "充满活力的特写编辑肖像，模特眼神犀利，头戴雕塑感帽子，色彩拼接丰富，眼部焦点锐利，景深较浅，具有Vogue杂志封面的美学风格，采用中画幅拍摄，工作室灯光效果强烈。",
    "size": "2K",
    "output_format": "png",
    "watermark": false
  }'
```

## 请求参数

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | ✅ | — | 固定为 `doubao-seedream-5.0-lite` |
| `prompt` | string | ✅ | — | 图像描述文本 |
| `size` | string | — | — | 输出尺寸档位：`2K`、`3K`、`4K` |
| `output_format` | string | — | — | 输出格式：`png`、`jpeg` |
| `watermark` | boolean | — | `false` | 是否添加水印 |

## 注意事项

- 必须使用 Agent Plan 专属 API Key 和 Base URL（含 `/plan`）
- 参考图数量 + 生成图片数量 ≤ 15 张
- 抵扣系数：1 张成功生成的图片 = 99 AFP

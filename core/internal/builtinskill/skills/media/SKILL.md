---
name: media
description: 多媒体生成（图片/语音/视频）的任务提交与查询——统一 media 工具的 action 用法、参数、异步轮询、成本护栏与 OpenAPI 端点
---

# 多媒体生成

图片 / 语音 / 视频三条能力共用一个 `media` 工具（对话内）与一组 OpenAPI 端点（脚本/快应用）。生成是**异步任务**：提交即返回，产物落盘后按路径交付。

## 第一步：看能力清单

system 的 `<media>` 段列出三类能力及当前生效模型：

- `available_media: 图片生成 - FLUX.1-schnell` → 可用
- `available_media: 语音合成 - （未配置，当前无此能力）` → **不可用**

能力未配置或被用户关闭时，直接如实回复“当前无语音合成能力”，提示用户去 **多媒体 · 模型与限额** 配置并启用；**不要试探调用、不要重试、不要用别的方式硬凑**。工具本身仍存在，调用会返回同样的友好提示。

轮内用户切换/启停模型时，`<resource_change>` 会更新能力清单。

## 对话内：media 工具

一个工具，按 `action` 分派：

| action | 必填 | 可选 | 说明 |
| --- | --- | --- | --- |
| `image` | `prompt` | `negative_prompt`、`image_size`（如 `1024x1024`）、`batch_size`（1-4，默认 1）、`seed` | 文生图，按张计费 |
| `audio` | `input`（≤5000 字符） | `voice`（alex/benjamin/charles/david/anna/bella/claire/diana，默认 alex）、`format`（mp3/wav/opus/pcm）、`speed`（0.25-4） | 语音合成，按输入计费 |
| `video` | `prompt`（镜头脚本，≤200 词） | `duration`（5-15 秒，默认 5）、`resolution`（480p/768p）、`ratio`（16:9/9:16/1:1）、`first_frame`、`last_frame`（本机路径或 http URL） | 文生视频，最贵 |
| `status` | — | `id`（省略=最近任务） | 查询任务/产物 |

提交后工具内部短等待约 15 秒：完成直接返回产物路径；未完成返回任务 id。之后隔 1-2 分钟用 `media {"action":"status","id":"..."}` 查一次，**不要连续快速轮询**（查询不加速上游生成）。视频通常数分钟，先做别的或告知用户预计耗时。

产物写在 `<工作目录>/tmp/`，用 `read_file` / 资源页查看；交付时给绝对路径或 `<supporter_url>` 入口。

## 成本与审批

- 三类都是贵价模型：**默认每次调用需要用户审批**，且有每日配额（图片按张、语音按次、视频按次/按秒）。
- 配额超限返回“今日配额已用完”，不要重试；提示用户明天再试或在 **多媒体 · 模型与限额** 调整。
- 非用户明确要求，不要批量放大 `batch_size`、拉长视频时长或反复微调重试。

## OpenAPI（脚本 / 快应用 / 外部程序）

鉴权、令牌获取与 SDK 见 **open-api 技能**。端点在 core 基址下：

| 端点 | 载荷 | 返回 |
| --- | --- | --- |
| `POST /api/open/image/call` | `{prompt, model?, negative_prompt?, image_size?, batch_size?, seed?, wait_seconds?}` | `{ok, job}` |
| `POST /api/open/audio/call` | `{input, model?, voice?, response_format?, speed?, wait_seconds?}` | `{ok, job}` |
| `POST /api/open/video/call` | `{prompt, model?, duration?, resolution?, ratio?, first_frame?, last_frame?, wait_seconds?}` | `{ok, job}` |
| `GET /api/open/media/jobs?limit=` | — | `{jobs}`（新→旧，清单不含 b64） |
| `GET /api/open/media/jobs/:id` | — | `{job}`（done 带 files / failed 带 err） |

- `wait_seconds` 默认 10、上限 60：只决定本次响应等多久，任务本身继续在后台跑。
- `job`：`{id, kind, status(queued|running|done|failed), model, brief, createdAt, files[], err, quota}`。`files[].path` 是产物本机绝对路径；图片/音频详情带 `b64`，视频带原始 `url`。
- 配额超限 `429`；参数/配置问题 `400`（含「当前无 X 能力」的说明）。

## 配置

模型在 **多媒体 · 模型与限额** 页维护（`media.json`）：每类可配多个模型，启用其一即获得该能力；关闭后工具保留、调用返回“当前无 X 能力”。不要让用户手改 `media.json`，指引到页面。

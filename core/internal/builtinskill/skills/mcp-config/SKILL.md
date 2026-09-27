---
name: mcp-config
description: 给应用增删改 MCP 服务器——直接编辑数据目录的 mcp.json，改动下一轮迭代自动热加载，无需重启
---

# MCP 服务器自配置

`<数据目录>/mcp.json`（数据目录绝对路径见 system 提示 `<workspace>` 段第一行）是 MCP 服务器的唯一配置来源。你可以直接编辑它：**保存后的下一轮迭代自动热加载**（连接重建），新工具面经 `<resource_change>` 反馈，无需重启应用。

## 配置文件结构

```json
{
  "servers": [
    {
      "name": "唯一标识",
      "description": "用途说明（给模型看，必写清楚）",
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": { "Authorization": "Bearer <key>" },
      "allow": ["tool_a", "tool_b"],
      "enabled": true
    }
  ]
}
```

字段规则：
- `type`：`http` | `sse` | `stdio`
- `http`/`sse` 型给 `url`（+可选 `headers` 鉴权）；`stdio` 型给 `command` + `args`（+可选 `env`）
- `allow` 是工具白名单，省略 = 全部允许；已有条目的 `allow` 必须原样保留
- `enabled` 省略 = 启用；禁用条目保留配置但不装配
- API key 放 `headers`/`env`，不要在对用户的回复里明文复述

## 操作流程

1. `read_file` 读 `<数据目录>/mcp.json`（绝对路径，从 `<workspace>` 段的数据目录行拼）
2. 解析后按需追加/修改/删除条目，**全量保留其余条目与所有字段**
3. `write_file` 整体写回（合法 JSON、缩进 2 空格）
4. 告知用户已配置；新工具下轮迭代即可用，无需其他操作

无法确定服务地址或鉴权方式时，先问用户，不要猜着配。

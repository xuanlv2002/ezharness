# ezharness上下文设计方案

## 特性1 会话管理
在整个设计中, 我希望ezharness的会话对用户是屏蔽的, 也就是用户是不需要newSession这种操作的。但这并不代表session不存在, 他只是使用了一种更隐藏式的方案来处理。

1. 正常的对话: 正常对话上下文存储在session内, session记录全部的记录。如果可以 我希望把session修改为otel格式去存储。这样 我可以去构建完整的调用链排除, 耗时跟踪等内容。 forktask的调用 单独存储在session-task-uuid文件夹里面。
2. 上下文组成 = 系统提示词(=系统描述 工具描述  hook注入系统提示词) + 输入输出
3. 上下文压缩hook处理方式: 当执行到上下文压缩 - 正确方案是总结当前的session内容 - 创建全新session - 总结内容注入到system 内, 并引用到上一次session路径, 支持compact 主动工具, 上下文压缩需要发送特殊事件前端渲染。forkAgent同样支持。压缩过的内容 可以在话题记忆看到(压缩内容 + 原始内容路径, fork task的session不在 属于主任务session的一部分)
4. systemPrompt管理: 每个session的systemPrompt是不变的, 代表就算是修改了记忆文件也只是会在下一次session的时候全部重新加载。skill,  mcp这些hook需要注入提示词到systemPrompt里面比如skill列表 mcp列表这样。
5. 状态栏: 每次用户chat时, 插入到用户输入的前面, 单独插入一条user记录 <agent_status></agent_status>, 记录内容: 当前时间  距离上次最后一条输出的时间  当前上下文占用/总量(推荐压缩 不推荐压缩) 当前可使用mcp(mcp名 + 前8个字描述)  变更:距离上一次对话, 可用skill的变更 可用mcp的变更记录。 这是必要的, 因为systemPromt是不修改的, 但是还在一轮会话内,例如用户通过页面新增了mcp, agent自己创建了新的skill, 下次对话 hook会重新加载这些内容。但是模型不知道 因为没有进入到systempromt 但是可用进入 agent_status, 标记处理, 例如新加: xxxskill descript   


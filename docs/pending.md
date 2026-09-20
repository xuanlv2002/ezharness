# 代办事项



## 6. 系统提示词重点优化(更新 重点/非必要/语法糖等) 【done】
## 8. tmp文件夹乱用
## 1. 快应用优化
## 2. 知识库开发
## 1. 上下文工程重点优化  把skill mcp 可用内容 放到里面 不放在系统提示词里面了。然后每次资源变更, 就重新给出可用skill和可用mcp。
## 2. mcp OAuth协议支持
## 3. mcp 能力优化
## 4. 默认携带skill注入

# 测试:
 ① system prompt 重写(新 session 生效)                                                                                   
  <workspace> 9 条路径罗列 → 3 个可写位 + 行为规则；新增 <action> 行动准则、<output> 输出规范(<$supper_url>
  正误示例、<@toolArg>);<mcp> 段加本机直调端点；人设加总纲句。                                                            
                                                                                                                          
  ② 六段 tool-guide 强化(ezloop 五段 + trim 段，每段扩成实践指导)
                                                                                                                          
  ③ trim 精致压缩：四节结构化摘要(【已完成】【正在做】【待办】【关键事实】)+ 每次整理重写
  sessions/<id>/progress.md;<session> 块告知该档案路径。
                                                 
  ④ 归档改沉淀式：归档前先调模型把会话内容与 memory/longterm/{user,projects,lessons}.md 现有内容合并重写(去重纠偏、≤30
  行、无新内容 UNCHANGED 跳过)，然后才是精炼交接摘要。
                                                                                                                          
  ⑤ skilltool 闭环(ezloop):load_skill 从 OnToolStart 拦截移进工具 Invoke。                                                
                                                                                                                          
  ⑥ 前端：trim 分割线改按【已完成】锚点(旧格式存档不再渲染摘要)；本会话早前还有 MCP 页修正(Tools=-1
  语义、三态按钮、表单溢出与美化)。

  你需要测的(按优先级)

  1. 新会话 system 检查(最容易出问题)：开新 session 问“复述你的行动准则和可点击入口格式”；顺手看
  sessions/<id>/session.json 里的 system——确认 <action>/<output> 在、没有 sessions/mcp.json 等路径残留
  2. 弱模型输出格式：让它交付 file:///term:// 入口，看 chip 渲染率是否改善
  3. trim:灌长对话到水位阈值(或直接叫它调 trim_context)→ 验证：时间线分割线一行摘要、progress.md
  四节齐全、接着问“我们做到哪了”看能否从档案续上
  4. 归档：点归档 → 三记忆文件被合并；马上再归档一次验证不膨胀；拿闲聊会话归档(应全 UNCHANGED、不写文件不报错)；新 session
   的 compact-summary 含沉淀说明
  5. load_skill 回归：让模型加载一个技能，返回应与之前一致(全文+目录树)
  6. MCP 快应用直调：让 AI 建个 fetch http://127.0.0.1:5262/api/mcp/call 的可观测页面(若你改过端口，段里 URL
  会跟着变，属正常)
  7. 旧存档回放：老会话翻页看 trim 分割线(旧格式会退化为无摘要的一行提示——你说过不用兼容，确认能接受即可)
# 代办事项
需求:
1. resource_change 和 agent_status的区别:
resource_change是主要用于资源变更等低频变更内容, 初始时就存储在了session里面的
agent_stauts 则主要用于每轮chat都变更 并且高频变换的东西, session内没有,每次动态获取
由于term 和 brown不是持久化状态, 每次启动都会关闭 所以不应该初始注入system 所以当前正在运行了哪些终端 和 brown 应当放在agent_status这个标签内的。
应该加上两个东西: 1. 当前开启浏览器tab:xx xx xx 2. 当前运行中终端xx xx xx

mcp_router设计:                                                                 
  1. 我希望mcp_router是一个系统级别的实例,全部的session                                                 
  全部的hook本质上是通过一个mcp_router进行的mcp调用。因为当前其实本身就是全局唯一的。                                     
  2. mcp的hook 需要传入这个mcp_router实例, 后面调用啊 list之类的全部都是用这一个。
  3. mcp_router我希望向外暴露出可以call的接口, 因为我有个大胆的想法: api->mcp tool-> 再把mcp tool作为api   
## 6. 系统提示词重点优化(更新 重点/非必要/语法糖等)
## 8. tmp文件夹乱用
## 1. 快应用优化
## 2. 知识库开发
## 1. 上下文工程重点优化  把skill mcp 可用内容 放到里面 不放在系统提示词里面了。然后每次资源变更, 就重新给出可用skill和可用mcp。
## 2. mcp OAuth协议支持
## 3. mcp 能力优化
## 4. 默认携带skill注入

测试:
                                                                                                                                                                                                           
  ① + ② + ⑤ 资源变更（破坏性重命名，不留兼容）                                            
  - <res_change> → <resource_change>，事件 res.change → resource.change，Go/前端/测试/文档全链路同步                         
  - 变更块现在附变更后完整清单：available_skill: 名 - 描述 / available_mcp: 名 - 描述 行（不带 -  前缀，前端变更卡天然忽略） 
  - system prompt 的 <skills>/<mcp> 块加禁令“以此为准，不要读 memory/skills 目录或 mcp.json 发现资源”；删除了“终端操作会进
  res_change”的残留谎言                                                                                                      
                                                                               
  ③ term/browser 工具                                   
  - 入参拆 name（简短标识）+ desc（详细描述），TermInfo/tab/desktop/WS/REST 全链路带 desc
  - 所有操作返回 JSON：终端 {id,name,desc,origin,exited,lastCmd,output,note}、浏览器
  {id,name,desc,origin,url,title,loading,note}，close 返回 {id,closed}，list 与操作同构
                                                                                                                                        
  - 浏览器：title 空 → (无标题)；browser_read 正文空 → (空页面) / 链接空 → (无链接)；desc
  未填时字段直接省略（可选字段语义明确）                                                                                     
  - 终端：term_send/term_start×输出空 →aoutput:"(无输出)"；term_read1无新增 → output:"(无新输出)"（note 不再重复）
  - available 清单：技能或 MCP 全部移除后输出 available_skill: （当前无可用技能） / available_mcp: （当前无可用 MCP          
  服务）；单项描述空 → （无描述）（system prompt 的 <skills>/<mcp> 块同步兜底，不再有 name -  空尾巴）                                                                                                                  
  ④ MCP                                                                                                                      
  - ezloop ext/hook/mcp/sdk.go 重写为 mark3labs/mcp-go v1.1.0，三种传输：StreamableHTTP / SSE / Stdio(command, env,          
  args...)（env 为附加变量，继承父进程环境合并）；go-sdk 依赖彻底移除；core 启用 replace（go 版本升至 1.25.5）
  - mcp.json 契约：type 三档 http|sse|stdio，stdio 独立 command/args/env（不再让 command 冒用 name，旧 stdio
  配置需改写）；错误链去掉 mcp.json: connect: ... 多层前缀
  - McpView：删除改自制居中确认框（同 MemoryView 样式）；persist
  链式串行化（排队前取快照，根治删除后无法添加的竞态）；添加/编辑表单三传输完整配置（stdio：command + args 每行一个 + env
  键值对）

  手工项（你自测）：连真实 MCP server（三种传输各一）、删除后立即添加、term_start/browser_tab 的 JSON
  返回、加技能后看变更卡。

验收:



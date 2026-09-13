# 代办事项
4. output前端特殊渲染语法<$supper_url></$supper_url> 前端可以进行解析, 并允许用户点击跳转/打开之类的。可以先实现占位, 但是具体怎么展示可以先不做, 因为我还没想好具体的类型有哪些, 跳转方案是什么。[done]
5. toolArg语法糖, 特殊字符自动转为内容, 减少agent参数调用。 例如工具参数传入 <@toolArg>xx/xx/xx.txt</toolArg> 就自动把xx.txt的内容作为工具参数。 [done]

6. 终端使用右展抽屉, 压缩输入框 [done]
6. 快应用扩展
9. 知识库处理
3. 浏览器 添加 [done]

1. 修复.exe启动后会打开一个空控制台问题 [done]

1. 浏览器 bug修复: 1.截图桥中断  2.应使用一个统一的抽屉, 点击后可以独立, 关闭回归抽屉, 抽屉内可以永远关闭。全局唯浏览器线程, 使用tab 栏控制页面切换 [done]
2. [4760:0913/005613.455:ERROR:net\socket\ssl_client_socket_impl.cc:962] handshake failed; returned -1, SSL error code 1, net_error -100 [done]

3. 统一 终端 资源看板 浏览器的操作逻辑: 先抽屉, 可拉出。用户操作不再回传了, 只有开关动作 加入到状态变更。
4. 把skill mcp 可用内容 放到<agent_status>里面 不放在系统提示词里面了。然后每次资源变更, 就重新给出可用skill和可用mcp。




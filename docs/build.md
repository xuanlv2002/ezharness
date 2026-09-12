# 构建方案

目标一句话：**一条三段构建链产出桌面应用，版本单源配置，产物按版本归档**。
开发与发布走同一条链，只是版本标识与产物目录不同。

## 三层结构与构建链

| 层 | 目录 | 构建 | 产物 |
|----|------|------|------|
| 前端页面 | `frontend/` | vite build（双入口 index.html + browser.html） | `frontend/dist/` |
| 内核 sidecar | `core/` | go build（`go:embed dist` 内嵌前端产物） | `ezharness-core.exe` |
| 桌面壳 | `desktop/` | electron-builder（extraResources 内嵌 core exe） | 安装包 + 绿色版 exe |

构建链（两个脚本共用）：

1. `frontend: npm run build` → `frontend/dist/`
2. 拷贝 `frontend/dist` → `core/dist`（gitignore，构建时生成）→ `go build -o ezharness-core.exe ./core`（仓库根）
3. `desktop: npx electron-builder` → `desktop/release/` 下产物，再归档到仓库根 `release/`

## 版本单源：script/version.yaml

```yaml
version: 0.1.2
```

脚本读取后同步两处（PowerShell 正则替换，勿手改 package.json 版本）：

- `frontend/package.json` ← `v<version>`：右下角页脚显示 `ezharness-v<version>`
  （dev 构建写 `dev`，页脚显示 `ezharness-dev`）
- `desktop/package.json` ← `<version>`：electron-builder 打包版本与产物命名用
- core 无版本内容，不参与同步

发新版只改 `version.yaml` 一处。

## 脚本（script/）

### dev.bat — 开发构建

三段构建链 + portable 打包，产出：

```
release\dev\ezharness-dev.exe    绿色版快照（页脚 ezharness-dev）
```

### release.bat — 发布打包

三段构建链 + NSIS/portable 双打包，产出：

```
release\v<version>\ezharness-v<version>-setup.exe    NSIS 安装包
release\v<version>\ezharness-v<version>.exe          绿色版 exe
```

两个脚本首次运行自动 `npm install`（frontend/desktop 缺 node_modules 时）。

## 运行形态与数据目录

原则：**exe 在哪运行，配置与数据就在哪生成**（core `config.Root()` = exe 目录）。

- `ezharness.json`（端口/窗口尺寸，默认 5260）与 `data/` 就地生成，零配置可启动
- 安装版：core 在 `resources/`，配置数据落在安装目录（NSIS 默认 user 作用域，
  无 UAC，保证可写）
- 绿色版 portable：electron-builder 运行器注入 `PORTABLE_EXECUTABLE_DIR`
  （exe 所在目录），desktop 据此给 core 传 `--root`，数据跟随 exe 而非自解压
  临时目录
- 开发：exe 构建在仓库根，配置数据就在仓库根生成

## 国内镜像（首次装依赖/打包）

```bat
set ELECTRON_MIRROR=https://npmmirror.com/mirrors/electron/
set ELECTRON_BUILDER_BINARIES_MIRROR=https://npmmirror.com/mirrors/electron-builder-binaries/
```

## 脚本编写约定

- bat 必须纯 ASCII + CRLF（cmd 默认 GBK 代码页，UTF-8 中文会乱码）
- PowerShell 5.1 读写含中文的 JSON 必须显式 `-Encoding UTF8`（默认按 ANSI 读）

## 验证链

改代码后的最小验证：

```bat
cd core && go build ./... && go vet ./...
cd frontend && npm run build
desktop 主进程 JS：node --check 过一遍
```

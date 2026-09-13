# 构建方案

目标一句话：**一条三段构建链产出桌面应用，版本单源配置，产物集中在 bin/ 与 release/**。
调试（dev.bat）只走前两段构建后直接 `npm run start`，发布（release.bat）走完整三段链打包。

## 三层结构与构建链

| 层 | 目录 | 构建 | 产物 |
|----|------|------|------|
| 前端页面 | `frontend/` | vite build（双入口 index.html + browser.html） | `frontend/dist/` |
| 内核 sidecar | `core/` | go build（前端产物拷入 `core/web/dist` 后 go:embed） | `bin/ezharness-core.exe` |
| 桌面壳 | `desktop/` | electron-builder（extraResources 内嵌 core exe） | `release/v<version>/` 安装包 + 绿色版 |

构建链：

1. `frontend: npm run build` → `frontend/dist/`
2. 拷贝 `frontend/dist` → `core/web/dist`（embed 专用目录，gitignore；go:embed 不能跨包目录引用，须拷一次）→ `cd core && go build -o ..\bin\ezharness-core.exe .`
3. `desktop: npx electron-builder` → 产物直接输出 `release/v<version>/`（仅 release.bat）

dev.bat 走完 1-2 后在 `desktop/` 里 `npm run start` 前台启动 Electron 壳（拉起 `bin/` 下 core exe，无打包）。

## 版本单源：script/version.yaml

```yaml
version: 0.1.2
```

release.bat 读取后同步两处（PowerShell 正则替换，勿手改 package.json 版本）：

- `frontend/package.json` ← `v<version>`：右下角页脚显示 `ezharness-v<version>`
- `desktop/package.json` ← `<version>`：electron-builder 打包版本与产物命名用
- core 无版本内容，不参与同步

发新版只改 `version.yaml` 一处。

## 脚本（script/）

### dev.bat — 调试运行

前端构建 + core 编译（构建链 1-2），然后 `desktop/` 里 `npm run start` 前台启动 Electron。无打包产物。

### release.bat — 发布打包

三段构建链 + NSIS/portable 双打包，产出：

```
release\v<version>\ezharness-v<version>-setup.exe    NSIS 安装包
release\v<version>\ezharness-v<version>.exe          绿色版 exe
```

## 运行形态与数据目录

原则：**exe 在哪运行，配置与数据就在哪生成**（core `config.Root()` = exe 目录）。

- `ezharness.json`（端口/窗口尺寸，默认 5260）与 `data/` 就地生成，零配置可启动
- 安装版：core 在 `resources/`，配置数据落在安装目录（NSIS 默认 user 作用域，
  无 UAC，保证可写）
- 绿色版 portable：electron-builder 运行器注入 `PORTABLE_EXECUTABLE_DIR`
  （exe 所在目录），desktop 据此给 core 传 `--root`，数据跟随 exe 而非自解压
  临时目录
- 开发：exe 在 `bin/`，配置数据就在 `bin/` 生成

## 国内镜像（首次装依赖/打包）

electron 本体与 NSIS/winCodeSign 等打包二进制默认从 GitHub 下载，国内极慢。已两层配置 npmmirror 镜像，通常无需再管：

- 用户级环境变量 `ELECTRON_MIRROR` / `ELECTRON_BUILDER_BINARIES_MIRROR`（setx 永久生效）
- `desktop/.npmrc`（npm config 形式，兜底 electron postinstall）

手动临时指定（如换机器）：

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

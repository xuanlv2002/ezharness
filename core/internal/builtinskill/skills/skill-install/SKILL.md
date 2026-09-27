---
name: skill-install
description: 安装新技能——SKILL.md 直接写文件、zip 包经检查后解压，都落在 memory/skills/ 下，下个新会话进清单
---

# 技能安装

技能 = `<技能库>/<目录>/SKILL.md`（+ 可选 `scripts/` 等子资源，由正文相对路径引用）。技能库 = `<数据目录>/memory/skills`，两个绝对路径都见 system 提示 `<workspace>` 段（数据目录行与技能库行）。写入后**下个新会话**进 `<skills>` 清单生效，当前会话不回填。

## 三种来源

**① 口述/整理出的技能**：直接 `write_file` 写 `<技能库>/<名>/SKILL.md`（绝对路径）。

**② 用户拖入的 zip**（路径在附件引用里）：
1. 先检查内容：`tar -tf <zip路径>`，列表里不得有 `..`、绝对路径或盘符（zip-slip 防护）
2. 解压：`tar -xf <zip路径> -C <技能库绝对路径>/`（Windows 自带 bsdtar，可直接解 zip）
3. 检查目录层级：SKILL.md 必须位于 `<技能库>/<技能目录>/SKILL.md`；若 zip 外面多套了一层目录就挪正

**③ 单文件 SKILL.md**：写入对应技能目录。

## SKILL.md 规范

- frontmatter 扁平字段：`name`（缺省回落目录名）、`description`（单行或块标量多行均可，描述写清"什么时候用"——清单匹配靠它）
- 正文是给模型看的操作指令：写目标、步骤、边界，不写实现原理
- `scripts/` 里的脚本用相对路径引用，执行时按需读取

装完告诉用户：技能名、一句话用途、"下个新会话生效"。

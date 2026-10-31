---
name: skill-install
description: 导入已有技能——口述整理成文、用户拖入的 zip 解压落位、单文件 SKILL.md 放置，进 memory/skills/ 下个新会话生效
---

# 技能导入

把**已存在**的技能装进 ezharness：用户口述的、拖入的 zip 包、单个 SKILL.md 文件。要写一个新技能 → 用 skill-build 技能。

## 判定

| 来源 | 走向 |
| --- | --- |
| 用户口述/对话里整理出的技能 | 本技能 · 来源① |
| 用户拖入 zip（路径在附件引用里） | 本技能 · 来源② |
| 单个 SKILL.md 文件 | 本技能 · 来源③ |
| 从零设计新技能 | skill-build |

## 做法

技能库 = `<数据目录>/memory/skills`（绝对路径见 system `<workspace>` 段技能库行），技能 = `<技能库>/<目录>/SKILL.md`（+ 可选 `scripts/` 等子资源）。写入后**下个新会话**进 `<skills>` 清单，当前会话不回填。

**① 口述/整理**：直接 `write_file` 写 `<技能库>/<名>/SKILL.md`（绝对路径）。

**② zip**：
1. 检查内容：`tar -tf <zip路径>`——列表里不得有 `..`、绝对路径或盘符（zip-slip 防护）
2. 解压：`tar -xf <zip路径> -C <技能库绝对路径>/`（Windows 自带 bsdtar 可解 zip）
3. 层级归位：SKILL.md 必须在 `<技能库>/<技能目录>/SKILL.md`；zip 多套一层目录就挪正

**③ 单文件**：写入对应技能目录。

## 验证

- `read_file` 回读 SKILL.md 存在且 frontmatter 可解析（name/description 齐全）
- 告知用户：技能名、一句话用途、**下个新会话生效**；停用/启用在记忆页

## 排错

| 症状 | 处置 |
| --- | --- |
| zip 列表里有 `..`/绝对路径 | 拒绝解压，告知用户内容可疑 |
| 装完清单里没有 | 当前会话不回填，新会话才生效；新会话仍没有则查目录层级与 frontmatter |
| 与内建技能同名 | 用户技能让位内建，改目录名 |

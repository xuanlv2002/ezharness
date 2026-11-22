/* 版本同步：script/version.yaml 是唯一来源；frontend 带 v 前缀（页脚展示），
   desktop 用裸 semver（electron-builder 打包版本）。 */
const fs = require('fs')

const version = /^version:\s*(\S+)/m.exec(fs.readFileSync('script/version.yaml', 'utf8'))[1]
for (const [file, prefix] of [
  ['core/frontend/package.json', 'v'],
  ['desktop/package.json', ''],
]) {
  const text = fs.readFileSync(file, 'utf8')
  fs.writeFileSync(file, text.replace(/"version":\s*"[^"]*"/, `"version": "${prefix}${version}"`))
}
console.log(`[version] ${version}`)

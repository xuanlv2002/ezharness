/* 发布产物校验和：对 release 目录下的安装包生成 SHA256SUMS-<platform>.txt
   （平台后缀避免三平台任务同时往 Release 传同名附件）。 */
const fs = require('fs')
const path = require('path')
const crypto = require('crypto')

const dir = process.argv[2]
if (!dir) {
  console.error('usage: node script/checksums.js <release-dir>')
  process.exit(1)
}

const exts = ['.exe', '.dmg', '.AppImage']
const files = fs
  .readdirSync(dir)
  .filter((f) => exts.some((e) => f.endsWith(e)))
  .filter((f) => fs.statSync(path.join(dir, f)).isFile())
  .sort()

const lines = files.map((f) => {
  const hash = crypto.createHash('sha256').update(fs.readFileSync(path.join(dir, f))).digest('hex')
  return `${hash}  ${f}`
})
const out = path.join(dir, `SHA256SUMS-${process.platform}.txt`)
fs.writeFileSync(out, lines.join('\n') + '\n')
console.log(`[checksums] ${out} (${files.length} files)`)

import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync, statSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 模板替换：导入中心下载的必须是仓库里的原始 .xlsx 静态文件，
// 括号内的中文说明只是给填表人看的备注，不是导出程序的原始字段。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appSource = readFileSync(resolve(frontendRoot, 'src/App.vue'), 'utf8')
const templatePath = resolve(frontendRoot, 'public/templates/pjsk-goods-import-template.xlsx')

test('导入中心的下载按钮指向仓库内的静态模板文件', () => {
  assert.match(
    appSource,
    /<a class="secondary-button template-download" href="\/templates\/pjsk-goods-import-template\.xlsx" download>下载标准模板<\/a>/,
  )
  // 不允许在前端重新生成一张“相似表格”。
  assert.ok(!appSource.includes('generateTemplate'), '前端不应重新生成模板')
})

test('模板下载旁给出括号说明', () => {
  assert.ok(
    appSource.includes('括号内为中文说明，括号外为原表字段。请保留原表结构并填写或粘贴数据，不要删除合并单元格或调整关键位置。'),
    '缺少括号说明文案',
  )
})

test('静态模板文件存在且是合法 xlsx', () => {
  const stats = statSync(templatePath)
  assert.ok(stats.size > 0, '模板文件为空')
  const data = readFileSync(templatePath)
  assert.equal(data.subarray(0, 2).toString('latin1'), 'PK', '模板不是 zip/xlsx 格式')
  // 内容指纹，防止模板被程序重新生成后悄悄替换。
  assert.equal(
    createHash('sha256').update(data).digest('hex'),
    'b3a3577f352ca8d3f3d8745b90645aab536f37c9608f8574aabd82580ef1e941',
  )
})

// 工作表名称与括号备注表头的校验在后端
// backend/internal/importpreview 的 TestShippedTemplateFileIsStandardStructure 中完成，
// 那里会真正解压并解析这份 xlsx。

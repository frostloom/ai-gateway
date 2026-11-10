import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
const source = readFileSync(new URL('../src/portal/types.ts', import.meta.url), 'utf8')
const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } }).outputText
const { parseTierQuota } = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'))
for (const invalid of ['null', '[]', '1', '"text"', '{"a":-1}', '{"a":"100"}', '{"":1}', '{"a":1.5}']) {
  assert.deepEqual(parseTierQuota(invalid), {}, 'unsafe legacy quota ' + invalid)
}
assert.deepEqual(parseTierQuota('{"轻量":1000}'), { '轻量': 1000 })
console.log('[PASS] 历史套餐额度数据校验')

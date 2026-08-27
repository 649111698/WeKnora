import test from 'node:test'
import assert from 'node:assert/strict'
import { parseEchartsOption } from './echartsShared'

test('parseEchartsOption accepts valid option JSON', () => {
  const option = parseEchartsOption(
    '{"title":{"text":"销量"},"xAxis":{"type":"category","data":["1月","2月"]},"series":[{"type":"bar","data":[10,20]}]}',
  )
  assert.ok(option)
  assert.equal((option.series as Array<{ type: string }>)[0].type, 'bar')
})

test('parseEchartsOption tolerates trailing commas', () => {
  const option = parseEchartsOption(
    '{"title":{"text":"销量",},"series":[{"type":"pie","data":[{"value":1,"name":"a"},],},],}',
  )
  assert.ok(option)
  assert.equal((option.series as Array<{ type: string }>)[0].type, 'pie')
})

test('parseEchartsOption rejects empty and non-object payloads', () => {
  assert.equal(parseEchartsOption(''), null)
  assert.equal(parseEchartsOption('   '), null)
  assert.equal(parseEchartsOption('not json at all'), null)
  assert.equal(parseEchartsOption('["array","is","not","an","option"]'), null)
  assert.equal(parseEchartsOption('42'), null)
})

test('parseEchartsOption keeps original text untouched when parsing fails', () => {
  // 确认容错分支不会改写无法解析的内容语义：只有尾随逗号场景被改写
  assert.equal(parseEchartsOption('{broken json,,,'), null)
})

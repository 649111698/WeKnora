import assert from 'node:assert/strict'
import test from 'node:test'

import {
  findSafeCut,
  isUniformRowData,
  planPageCuts,
} from './answerPdfPagination.ts'

// 构造一行 RGBA 像素：uniform 行整行同色；text 行在中部埋一个差异像素
function makeRow(width: number, kind: 'uniform' | 'text', base = 255): Uint8ClampedArray {
  const data = new Uint8ClampedArray(width * 4)
  for (let x = 0; x < width; x++) {
    data[x * 4] = base
    data[x * 4 + 1] = base
    data[x * 4 + 2] = base
    data[x * 4 + 3] = 255
  }
  if (kind === 'text') {
    const mid = Math.floor(width / 2) * 4
    data[mid] = base - 80 // 文字像素，远超 ±10 容差
  }
  return data
}

function makeProbe(width: number, textRows: Set<number>) {
  return (y: number) => isUniformRowData(makeRow(width, textRows.has(y) ? 'text' : 'uniform'))
}

test('isUniformRowData: 整行同色为安全行', () => {
  assert.equal(isUniformRowData(makeRow(40, 'uniform')), true)
  assert.equal(isUniformRowData(makeRow(40, 'text')), false)
  // 抗锯齿级别的小噪点（±10 内）不视为文字
  const noisy = makeRow(40, 'uniform')
  noisy[8] = 255 - 10
  noisy[9] = 255 - 9
  assert.equal(isUniformRowData(noisy), true)
})

test('findSafeCut: 从理想切点向上回扫，取最靠下的安全切点', () => {
  // 行 100-109 是文字，理想切点 108 落在文字上；band=2 要求切点 y 的
  // y、y+1 两行都空白，所以切在 98（文字块上方最近的间隙）
  const textRows = new Set<number>()
  for (let y = 100; y <= 109; y++) textRows.add(y)
  const probe = makeProbe(40, textRows)
  assert.equal(findSafeCut(probe, 60, 108, 2), 98)
})

test('findSafeCut: 窗口内没有安全行时返回 null（调用方退回硬切）', () => {
  const textRows = new Set<number>()
  for (let y = 60; y <= 130; y++) textRows.add(y) // 整段跨页图片/代码块
  const probe = makeProbe(40, textRows)
  assert.equal(findSafeCut(probe, 60, 130, 2), null)
})

test('planPageCuts: 切点避开文字行且最后一项为图高', () => {
  // 模拟：pageSrcH=200，第一页理想切点 200 落在文字上，
  // 上方最近的行间隙在 190
  const imgH = 350
  const textRows = new Set<number>()
  for (let y = 191; y <= 200; y++) textRows.add(y)
  const cuts = planPageCuts(imgH, 200, { lookback: 44, minAdvance: 60, band: 2 }, makeProbe(40, textRows))

  assert.equal(cuts[cuts.length - 1], imgH)
  // 回扫到文字块（191-200）上方最近的间隙；band=2 使切点落在 189
  assert.equal(cuts[0], 189)
  assert.ok(cuts.length >= 2)
  // 相邻切点构成页区间，每页至少 minAdvance
  let prev = 0
  for (const cut of cuts) {
    assert.ok(cut - prev >= 60, `页高 ${cut - prev} 低于最小页高`)
    prev = cut
  }
})

test('planPageCuts: 无安全行时按理想页高硬切（旧行为兜底）', () => {
  const imgH = 450
  const textRows = new Set<number>()
  for (let y = 0; y < imgH; y++) textRows.add(y) // 整图无空白
  const cuts = planPageCuts(imgH, 200, { lookback: 44, minAdvance: 60, band: 2 }, makeProbe(40, textRows))
  assert.deepEqual(cuts, [200, 400, 450])
})

test('planPageCuts: 不足一页时只有一页', () => {
  const cuts = planPageCuts(150, 200, { lookback: 44, minAdvance: 60, band: 2 }, makeProbe(40, new Set()))
  assert.deepEqual(cuts, [150])
})

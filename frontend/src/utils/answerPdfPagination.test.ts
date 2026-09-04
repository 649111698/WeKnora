import assert from 'node:assert/strict'
import test from 'node:test'

import {
  findSafeCut,
  isMostlyUniformRowData,
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

// 构造带稀疏离群像素的行：模拟表格行间隙中贯穿的竖向 1px 边框线
function makeHairlineRow(width: number, lines: number, base = 255): Uint8ClampedArray {
  const data = makeRow(width, 'uniform', base)
  const stride = Math.floor(width / lines)
  for (let k = 0; k < lines; k++) {
    const x = Math.floor(k * stride + stride / 2)
    data[x * 4] = base - 120
    data[x * 4 + 1] = base - 120
    data[x * 4 + 2] = base - 120
  }
  return data
}

test('isMostlyUniformRowData: 表格行间隙的竖向边框线仍视为安全切点', () => {
  // 1600px 宽（2x A4）、15 条边框线 ≈ 0.9% 离群
  assert.equal(isMostlyUniformRowData(makeHairlineRow(1600, 15)), true)
})

test('isMostlyUniformRowData: 文字行不可切', () => {
  // 40px 行、4 个文字像素 = 10% 离群，远超阈值
  const row = makeRow(40, 'uniform')
  for (const x of [10, 15, 20, 25]) {
    row[x * 4] = 100
    row[x * 4 + 1] = 100
    row[x * 4 + 2] = 100
  }
  assert.equal(isMostlyUniformRowData(row), false)
})

test('isMostlyUniformRowData: 行首像素落在边框线上不影响判定', () => {
  const row = makeHairlineRow(1600, 15)
  row[0] = row[1] = row[2] = 100 // 行首即边框色
  assert.equal(isMostlyUniformRowData(row), true)
})

test('isMostlyUniformRowData: 纯色行安全', () => {
  assert.equal(isMostlyUniformRowData(makeRow(100, 'uniform')), true)
})

test('PDF 分页探测使用基本同色判定：表格文字行之间可翻页', () => {
  // 模拟表格：文字行(y=100-112)与下一行文字(y=124-136)之间是仅含
  // 竖向边框线的间隙(y=113-123)；理想切点 122 落在间隙内
  const width = 1600
  const textRows = new Set<number>()
  for (let y = 100; y <= 112; y++) textRows.add(y)
  for (let y = 124; y <= 136; y++) textRows.add(y)
  const probe = (y: number) =>
    isMostlyUniformRowData(textRows.has(y) ? makeRow(width, 'text') : makeHairlineRow(width, 15))
  // band=2：切点 y 与 y+1 都必须是"边框线间隙"；自下而上扫描取最靠下
  // 的安全切点 120（122-2），保证每页尽量填满
  assert.equal(findSafeCut(probe, 80, 122, 2), 120)
})

/**
 * 回答导出 PDF 的智能分页算法（纯函数，无 DOM 依赖）。
 *
 * 整条回答先渲染成一张长图，再切成 A4 页。固定高度硬切会把压在分页线
 * 上的文字行切成上下两半；这里在理想切点附近自下而上回扫，找一段整行
 * 同色的像素（行间空白/段落间隙/纯色块内空行）作为实际切点，找不到才
 * 退回硬切。
 */

// 抗锯齿容差：±10/255，吸收边缘像素噪点
const UNIFORM_TOLERANCE = 10

// 单行 RGBA 像素是否整行同色。行内只要有一个文字/图片像素即返回 false。
export function isUniformRowData(data: Uint8ClampedArray): boolean {
  const r = data[0]
  const g = data[1]
  const b = data[2]
  for (let x = 4; x < data.length; x += 4) {
    if (
      Math.abs(data[x] - r) > UNIFORM_TOLERANCE ||
      Math.abs(data[x + 1] - g) > UNIFORM_TOLERANCE ||
      Math.abs(data[x + 2] - b) > UNIFORM_TOLERANCE
    ) {
      return false
    }
  }
  return true
}

// 单行像素"基本同色"判定：以行内主导色为基准，允许极少量离群像素
// （表格行间隙里贯穿的竖向 1px 边框线），离群占比超过阈值说明行内
// 有文字/图形，不能作为安全切点。PDF 分页用它探测，使表格能在行间
// 隙处翻页，而不是把某一行切成上下两半。
export function isMostlyUniformRowData(
  data: Uint8ClampedArray,
  maxOutlierRatio = 0.02
): boolean {
  const total = data.length / 4
  if (!total) return true
  // 采样估计主导色（每通道量化到 16 级），避免行首像素恰好落在边框线上
  const counts = new Map<number, number>()
  const sampleStride = Math.max(1, Math.floor(total / 240))
  for (let x = 0; x < total; x += sampleStride) {
    const i = x * 4
    const key = ((data[i] >> 4) << 8) | ((data[i + 1] >> 4) << 4) | (data[i + 2] >> 4)
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  let dominantKey = 0
  let dominantCount = -1
  for (const [key, count] of counts) {
    if (count > dominantCount) {
      dominantCount = count
      dominantKey = key
    }
  }
  // 用主导桶内首个像素的原始值做精确基准，消除量化误差
  let baseR = data[0]
  let baseG = data[1]
  let baseB = data[2]
  for (let x = 0; x < total; x += sampleStride) {
    const i = x * 4
    const key = ((data[i] >> 4) << 8) | ((data[i + 1] >> 4) << 4) | (data[i + 2] >> 4)
    if (key === dominantKey) {
      baseR = data[i]
      baseG = data[i + 1]
      baseB = data[i + 2]
      break
    }
  }
  let outliers = 0
  for (let x = 0; x < total; x++) {
    const i = x * 4
    if (
      Math.abs(data[i] - baseR) > UNIFORM_TOLERANCE ||
      Math.abs(data[i + 1] - baseG) > UNIFORM_TOLERANCE ||
      Math.abs(data[i + 2] - baseB) > UNIFORM_TOLERANCE
    ) {
      outliers++
    }
  }
  return outliers / total <= maxOutlierRatio
}

// 在 (from, to] 内自下而上找连续 band 行都"安全"的切点；找不到返回 null。
// 从理想页高位置向上回扫，优先取最靠下的安全切点，保证每页尽量填满。
export function findSafeCut(
  isSafeRow: (y: number) => boolean,
  from: number,
  to: number,
  band: number
): number | null {
  for (let y = to - band; y > from; y--) {
    let uniform = true
    for (let dy = 0; dy < band && uniform; dy++) {
      if (!isSafeRow(y + dy)) uniform = false
    }
    if (uniform) return y
  }
  return null
}

export interface PageCutOptions {
  // 自理想切点向上最多回扫的像素行数
  lookback: number
  // 每页至少要装的内容高度，防止回扫切出接近空页
  minAdvance: number
  // 连续多少行同色才算安全切点
  band: number
}

// 计算整图的切点序列，返回值首项为第一页页底，相邻项构成页区间，
// 最后一项恒为 imgH。
export function planPageCuts(
  imgH: number,
  pageSrcH: number,
  opts: PageCutOptions,
  isSafeRow: (y: number) => boolean
): number[] {
  const cuts: number[] = []
  let y = 0
  while (y < imgH) {
    const ideal = Math.floor(y + pageSrcH)
    let cut = Math.min(ideal, imgH)
    const minCut = Math.max(y + opts.minAdvance, ideal - opts.lookback)
    // 只在"切完这页后面还有整页内容"时回扫：最后一页直接切到底，否则
    // 安全切点会把尾巴撕成一条几乎空白的页。接受的安全切点也必须给
    // 后面留够一页的量。
    if (ideal < imgH && minCut < cut - opts.band) {
      const safe = findSafeCut(isSafeRow, Math.floor(minCut), cut, opts.band)
      if (safe !== null && imgH - safe >= opts.minAdvance) cut = safe
    }
    if (cut <= y) {
      cut = Math.min(ideal, imgH) // 防御：绝不因切点异常而死循环
    }
    cuts.push(cut)
    y = cut
  }
  return cuts
}

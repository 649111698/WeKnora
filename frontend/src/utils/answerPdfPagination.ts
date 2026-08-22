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

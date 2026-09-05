import type { Tokens } from 'marked'
import hljs from 'highlight.js'
import 'highlight.js/styles/github.css'
import { openMermaidFullscreen } from '@/utils/mermaidViewer.ts'
import {
  buildCodeBlockHtml,
  buildMermaidBlockHtml,
  attachMarkdownEnhancementListeners,
  highlightCodeBlocksInContainer,
  syncMermaidExpandButtons,
} from '@/utils/markdownEnhancements'

hljs.registerAliases('mermaid', { languageName: 'plaintext' })

let mermaidMod: typeof import('mermaid') | null = null
let mermaidInitialized = false
let initPromise: Promise<void> | null = null

// ECharts 5 default theme tokens (see echarts/theme default):
//   series palette #5470C6 #91CC75 #FAC858 #EE6666 #73C0DE #3BA272 #FC8452
//   #9A60B4 #EA7CCC; title #464646; legend text #333333; axis label/axis line
//   #6E7074; splitLine #E0E6F1. Node/task fills take the series palette with
//   white text (ECharts graph/bar look), borders match the fill (borderless
//   series style), pie slices are opaque with white gaps.
const MERMAID_LIGHT_THEME = {
  darkMode: false,
  background: '#ffffff',
  primaryColor: '#5470C6',
  primaryTextColor: '#FFFFFF',
  primaryBorderColor: '#5470C6',
  secondaryColor: '#91CC75',
  secondaryTextColor: '#FFFFFF',
  secondaryBorderColor: '#91CC75',
  tertiaryColor: '#FAC858',
  tertiaryTextColor: '#464646',
  tertiaryBorderColor: '#FAC858',
  lineColor: '#6E7074',
  textColor: '#333333',
  classText: '#FFFFFF',
  mainBkg: '#5470C6',
  // 类图文字填充优先取 nodeBorder（fill: nodeBorder || classText），实心
  // 节点需要它保持白色；顺带给实心节点一圈 1px 白描边（ECharts 饼图缝
  // 隙/柱体分隔同款语言）。
  nodeBorder: '#FFFFFF',
  clusterBkg: '#F7F8FA',
  clusterBorder: '#E0E6F1',
  titleColor: '#464646',
  edgeLabelBackground: '#ffffff',
  actorBorder: '#5470C6',
  actorBkg: '#5470C6',
  actorTextColor: '#FFFFFF',
  actorLineColor: '#6E7074',
  signalColor: '#6E7074',
  signalTextColor: '#333333',
  labelBoxBkgColor: '#EBF0FA',
  labelBoxBorderColor: '#5470C6',
  labelTextColor: '#464646',
  loopTextColor: '#464646',
  noteBkgColor: '#F7F8FA',
  noteTextColor: '#464646',
  noteBorderColor: '#E0E6F1',
  activationBkgColor: '#EBF0FA',
  activationBorderColor: '#5470C6',
  // Gantt
  sectionBkgColor: '#FAFBFC',
  altSectionBkgColor: '#ffffff',
  gridColor: '#E0E6F1',
  todayLineColor: '#EE6666',
  taskBorderColor: '#5470C6',
  taskBkgColor: '#5470C6',
  taskTextLightColor: '#FFFFFF',
  taskTextDarkColor: '#464646',
  activeTaskBorderColor: '#91CC75',
  activeTaskBkgColor: '#91CC75',
  doneTaskBkgColor: '#73C0DE',
  doneTaskBorderColor: '#73C0DE',
  critBkgColor: '#EE6666',
  critBorderColor: '#EE6666',
  fontSize: '14px',
  // Categorical chart palette (pie slices): ECharts series palette, then
  // lightened repeats for slices 10-12. pieOpacity defaults to 0.7 — ECharts
  // pie slices are opaque with white borders between them.
  pie1: '#5470C6',
  pie2: '#91CC75',
  pie3: '#FAC858',
  pie4: '#EE6666',
  pie5: '#73C0DE',
  pie6: '#3BA272',
  pie7: '#FC8452',
  pie8: '#9A60B4',
  pie9: '#EA7CCC',
  pie10: '#A1B6E6',
  pie11: '#BCE0AC',
  pie12: '#FCD98F',
  pieStrokeColor: '#FFFFFF',
  pieOuterStrokeColor: '#FFFFFF',
  pieOpacity: '1',
  pieTitleTextColor: '#464646',
  pieSectionTextColor: '#FFFFFF',
  pieLegendTextColor: '#333333',
  // xychart-beta series palette — mermaid reads xyChart.plotColorPalette from
  // themeVariables (not top-level config); keep it in sync with the pie slices.
  // Axis lines/ticks/labels use the ECharts axis gray (#6E7074).
  xyChart: {
    backgroundColor: '#FFFFFF',
    titleColor: '#464646',
    xAxisLabelColor: '#6E7074',
    xAxisTitleColor: '#464646',
    xAxisTickColor: '#6E7074',
    xAxisLineColor: '#6E7074',
    yAxisLabelColor: '#6E7074',
    yAxisTitleColor: '#464646',
    yAxisTickColor: '#6E7074',
    yAxisLineColor: '#6E7074',
    plotColorPalette: '#5470C6,#91CC75,#FAC858,#EE6666,#73C0DE,#3BA272,#FC8452,#9A60B4',
  },
}

// Dark mirror of the light ECharts theme: the same ECharts hue set, with the
// blue/teal/purple series brightened for the app's #1a1f28 canvas; pie gaps
// and xy axes follow the dark equivalents (canvas-colored slice borders,
// lightened axis gray).
const MERMAID_DARK_THEME = {
  darkMode: true,
  background: '#1a1f28',
  primaryColor: '#6C8FE8',
  primaryTextColor: '#FFFFFF',
  primaryBorderColor: '#6C8FE8',
  secondaryColor: '#91CC75',
  secondaryTextColor: '#FFFFFF',
  secondaryBorderColor: '#91CC75',
  tertiaryColor: '#FAC858',
  tertiaryTextColor: '#333840',
  tertiaryBorderColor: '#FAC858',
  lineColor: '#8E959F',
  textColor: '#E2E8F0',
  classText: '#FFFFFF',
  mainBkg: '#6C8FE8',
  // 同浅色版：类图文字与节点描边走 nodeBorder。
  nodeBorder: '#FFFFFF',
  clusterBkg: '#1a2332',
  clusterBorder: '#3E4655',
  titleColor: '#f1f5f9',
  edgeLabelBackground: '#1e293b',
  actorBorder: '#6C8FE8',
  actorBkg: '#6C8FE8',
  actorTextColor: '#FFFFFF',
  actorLineColor: '#8E959F',
  signalColor: '#8E959F',
  signalTextColor: '#E2E8F0',
  labelBoxBkgColor: '#28324A',
  labelBoxBorderColor: '#6C8FE8',
  labelTextColor: '#E2E8F0',
  loopTextColor: '#CBD5E1',
  noteBkgColor: '#272E3B',
  noteTextColor: '#CBD5E1',
  noteBorderColor: '#3E4655',
  activationBkgColor: '#28324A',
  activationBorderColor: '#6C8FE8',
  // Gantt
  sectionBkgColor: '#1a2332',
  altSectionBkgColor: '#1e293b',
  gridColor: '#334155',
  todayLineColor: '#EE6666',
  taskBorderColor: '#6C8FE8',
  taskBkgColor: '#6C8FE8',
  taskTextLightColor: '#FFFFFF',
  taskTextDarkColor: '#1F2937',
  activeTaskBorderColor: '#91CC75',
  activeTaskBkgColor: '#91CC75',
  doneTaskBkgColor: '#3E4A6E',
  doneTaskBorderColor: '#5A6A96',
  critBkgColor: '#EE6666',
  critBorderColor: '#EE6666',
  fontSize: '14px',
  // ECharts palette brightened for dark backgrounds; 10-12 lighten further.
  pie1: '#6C8FE8',
  pie2: '#91CC75',
  pie3: '#FAC858',
  pie4: '#EE6666',
  pie5: '#73C0DE',
  pie6: '#4CBF8F',
  pie7: '#FC8452',
  pie8: '#AC85CE',
  pie9: '#EA7CCC',
  pie10: '#9DADF0',
  pie11: '#B9E5A9',
  pie12: '#FBD98F',
  pieStrokeColor: '#1A1F28',
  pieOuterStrokeColor: '#1A1F28',
  pieOpacity: '1',
  pieTitleTextColor: '#F1F5F9',
  pieSectionTextColor: '#FFFFFF',
  pieLegendTextColor: '#E2E8F0',
  // Brightened xychart-beta series palette for dark backgrounds.
  xyChart: {
    backgroundColor: 'transparent',
    titleColor: '#F1F5F9',
    xAxisLabelColor: '#9AA3B2',
    xAxisTitleColor: '#E2E8F0',
    xAxisTickColor: '#8E959F',
    xAxisLineColor: '#8E959F',
    yAxisLabelColor: '#9AA3B2',
    yAxisTitleColor: '#E2E8F0',
    yAxisTickColor: '#8E959F',
    yAxisLineColor: '#8E959F',
    plotColorPalette: '#6C8FE8,#91CC75,#FAC858,#EE6666,#73C0DE,#4CBF8F,#FC8452,#AC85CE',
  },
}

function resolveMermaidThemeVariables() {
  const isDark = document.documentElement.getAttribute('theme-mode') === 'dark'
  return isDark ? MERMAID_DARK_THEME : MERMAID_LIGHT_THEME
}

const MERMAID_CONFIG = {
  startOnLoad: false,
  theme: 'base' as const,
  securityLevel: 'strict' as const,
  fontFamily: 'PingFang SC, Microsoft YaHei, sans-serif',
  flowchart: {
    useMaxWidth: true,
    htmlLabels: true,
    curve: 'basis',
    padding: 16,
  },
  sequence: {
    useMaxWidth: true,
    diagramMarginX: 12,
    diagramMarginY: 12,
    actorMargin: 56,
    width: 156,
    height: 68,
    boxMargin: 10,
  },
  gantt: {
    useMaxWidth: true,
    leftPadding: 80,
    gridLineStartPadding: 40,
    barHeight: 22,
    barGap: 6,
    topPadding: 56,
  },
  er: {
    useMaxWidth: true,
  },
  journey: {
    useMaxWidth: true,
  },
}

async function getMermaid() {
  if (!mermaidMod) {
    mermaidMod = await import('mermaid')
  }
  return mermaidMod.default
}

function escapeHtml(text: string) {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function highlightCode(text: string, lang?: string | null) {
  const language = (lang || '').trim()
  if (language && hljs.getLanguage(language)) {
    try {
      const result = hljs.highlight(text, { language })
      return { html: result.value, language: result.language || language }
    } catch {
      // fall through
    }
  }
  const auto = hljs.highlightAuto(text, language ? [language] : undefined)
  return { html: auto.value, language: auto.language || language || 'plaintext' }
}

export const ensureMermaidInitialized = () => {
  if (!initPromise) {
    initPromise = (async () => {
      const mermaid = await getMermaid()
      if (!mermaidInitialized) {
        mermaid.initialize({
          ...MERMAID_CONFIG,
          themeVariables: resolveMermaidThemeVariables(),
        } as Parameters<typeof mermaid.initialize>[0])
        mermaidInitialized = true
      }
    })()
  }
}

let mermaidCount = 0

export const createMermaidCodeRenderer = (idPrefix: string) => {
  return ({ text, lang }: Tokens.Code) => {
    const { html: highlighted, language: highlightLang } = highlightCode(text, lang)
    if (lang === 'mermaid') {
      const id = `${idPrefix}-${++mermaidCount}`
      const inner = `<code class="hljs language-${highlightLang}">${highlighted}</code>`
      return buildMermaidBlockHtml(inner, `id="${id}" data-mermaid="false"`)
    }
    return buildCodeBlockHtml(lang || highlightLang, highlighted, highlightLang)
  }
}

export const renderMermaidToSvg = async (code: string, id?: string): Promise<string | null> => {
  if (!code.trim()) return null
  try {
    const mermaid = await getMermaid()
    ensureMermaidInitialized()
    await initPromise
    await mermaid.parse(code)
    // Mermaid reuses the render id as the SVG root id. Always mint a fresh
    // one so a later render cannot collide with an SVG already in the DOM.
    const renderId = `${id || 'mermaid'}-${++mermaidCount}`
    const { svg } = await mermaid.render(renderId, code)
    return patchEdgeLabelContrast(svg)
  } catch (e) {
    console.error('Mermaid rendering error:', e)
    return null
  }
}

// Mermaid 没有独立的边线标签文字色变量：`.label` 的 color 取
// primaryTextColor（实心色节点需要白字），流程图的边线标签（是/否等）
// 继承同一规则，白色标签底上就是白字。在渲染出的 SVG 自带的 <style>
// 末尾追加一条 !important 规则兜底，文字色对齐 ECharts 图例灰。
// 规则用 SVG 根 id 作用域，避免同页多图（如明暗混排的缓存）互相覆盖；
// 只命中 .edgeLabel（流程图/状态图的边线标签），不影响节点、饼图、
// 时序图等其它文本。
const patchEdgeLabelContrast = (svg: string): string => {
  const m = /<svg[^>]*\sid="([^"]+)"/.exec(svg)
  if (!m || !svg.includes('</style>')) return svg
  const isDark = document.documentElement.getAttribute('theme-mode') === 'dark'
  const text = isDark ? '#E2E8F0' : '#333333'
  const scope = `#${m[1]}`
  const rules = `${scope} .edgeLabel,${scope} .edgeLabel *{color:${text} !important;fill:${text} !important;}`
  return svg.replace('</style>', rules + '</style>')
}

// Mermaid lays out some diagrams (pie titles especially) with near-zero
// margins around text: the declared viewBox hugs the measured text bounds.
// When the runtime font renders slightly wider than the metrics used at
// layout time (mobile WebViews, CJK fallback fonts), the leftmost glyphs
// end up outside the viewBox and get clipped — e.g. the first characters of
// a long pie title. Expand the viewBox (and the inline max-width that
// carries useMaxWidth sizing) to the actual content bounds so overflow
// can never clip. Idempotent up to a small tolerance: getBBox jitters by
// a few tenths of a unit with the viewBox's effective scale, and repeated
// enhance passes must not churn the attribute for sub-pixel deltas.
export const fitMermaidSvgViewport = (svg: SVGSVGElement | null): void => {
  if (!svg) return
  const vbAttr = svg.getAttribute('viewBox')
  if (!vbAttr) return
  const vb = svg.viewBox.baseVal
  if (!vb || vb.width <= 0 || vb.height <= 0) return
  let bounds: DOMRect
  try {
    bounds = svg.getBBox()
  } catch {
    return
  }
  if (!bounds || (!bounds.width && !bounds.height)) return
  const pad = 2
  const tol = 0.5
  const minX = Math.min(vb.x, bounds.x - pad)
  const minY = Math.min(vb.y, bounds.y - pad)
  const maxX = Math.max(vb.x + vb.width, bounds.x + bounds.width + pad)
  const maxY = Math.max(vb.y + vb.height, bounds.y + bounds.height + pad)
  if (
    minX >= vb.x - tol && minY >= vb.y - tol &&
    maxX <= vb.x + vb.width + tol && maxY <= vb.y + vb.height + tol
  ) return
  svg.setAttribute('viewBox', `${minX} ${minY} ${maxX - minX} ${maxY - minY}`)
  const style = svg.getAttribute('style') || ''
  if (/max-width:\s*[\d.]+px/.test(style)) {
    svg.setAttribute(
      'style',
      style.replace(/max-width:\s*[\d.]+px/, `max-width: ${Math.round(maxX - minX)}px`),
    )
  }
}

const fitMermaidSvgsInContainer = (rootElement: HTMLElement) => {
  rootElement
    .querySelectorAll<SVGSVGElement>('.chat-mermaid-block__canvas svg, pre.mermaid svg')
    .forEach((svg) => {
      fitMermaidSvgViewport(svg)
      capMermaidSvgDisplayWidth(svg)
    })
}

// Mermaid 的 useMaxWidth 机制会在 svg 上写内联 `max-width: Npx`（N 为
// 图表自然宽度）。放任内联声明生效，超宽图会溢出容器；用样式表
// `max-width: 100%` 强压（!important），小于容器的图又会被拉满整行、
// 显得远大于正文。把内联声明改写成 `width: min(Npx, 100%)` 一并表达
// "保持自然尺寸"与"不超容器"两个约束。幂等：改写后 style 里不再有
// max-width 声明，重复增强不会二次处理。
export const capMermaidSvgDisplayWidth = (svg: SVGSVGElement | null): void => {
  if (!svg) return
  const style = svg.getAttribute('style') || ''
  const m = /max-width:\s*([\d.]+)px/.exec(style)
  if (!m) return
  const natural = Math.round(parseFloat(m[1]))
  if (!(natural > 0)) return
  svg.setAttribute(
    'style',
    style.replace(/max-width:\s*[\d.]+px/, `width: min(${natural}px, 100%)`),
  )
}

function mermaidSourceFromElement(el: HTMLElement): string {
  const codeEl = el.querySelector('code')
  return (codeEl?.textContent ?? el.textContent ?? '').trim()
}

export async function appendMermaidSvgCache(
  codes: string[],
  cache: readonly string[],
  idPrefix: string,
): Promise<string[]> {
  const next = cache.slice()
  while (next.length < codes.length) {
    const i = next.length
    const svg = await renderMermaidToSvg(codes[i], `${idPrefix}-${i}`)
    next.push(svg || '')
  }
  return next
}

export const renderMermaidInContainer = async (
  rootElement: HTMLElement | null | undefined,
) => {
  if (!rootElement) return

  const mermaid = await getMermaid()
  ensureMermaidInitialized()
  await initPromise

  const mermaidElements = rootElement.querySelectorAll<HTMLElement>(
    'pre[data-mermaid="false"], .chat-mermaid-block__canvas[data-mermaid="false"]',
  )
  for (const el of mermaidElements) {
    try {
      if (el.querySelector('svg')) {
        el.setAttribute('data-mermaid', 'true')
        continue
      }
      const code = mermaidSourceFromElement(el)
      if (!code) continue
      await mermaid.parse(code)
      const renderId = `mermaid-render-${++mermaidCount}`
      const { svg } = await mermaid.render(renderId, code)
      el.classList.add('mermaid')
      el.innerHTML = patchEdgeLabelContrast(svg)
      el.setAttribute('data-mermaid', 'true')
      const rendered = el.querySelector('svg')
      fitMermaidSvgViewport(rendered)
      capMermaidSvgDisplayWidth(rendered)
    } catch (e) {
      console.error('Mermaid rendering error:', e)
      continue
    }
  }
}

export async function enhanceMarkdownContainer(
  rootElement: HTMLElement | null | undefined,
): Promise<void> {
  if (!rootElement) return
  attachMarkdownEnhancementListeners(rootElement)
  highlightCodeBlocksInContainer(rootElement)
  await renderMermaidInContainer(rootElement)
  // Covers SVGs injected via the cached-string path (v-html), which
  // renderMermaidInContainer skips because they are already rendered.
  fitMermaidSvgsInContainer(rootElement)
  syncMermaidExpandButtons(rootElement)
}

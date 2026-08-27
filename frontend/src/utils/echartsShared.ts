import type * as EChartsType from 'echarts/core'

function escapeHtml(text: string) {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

// echarts 按需模块懒加载：只有页面真正出现 echarts 块时才拉取，
// 避免把图表库打进主包。
let echartsPromise: Promise<typeof EChartsType> | null = null

async function getEcharts(): Promise<typeof EChartsType> {
  if (!echartsPromise) {
    echartsPromise = (async () => {
      const [{ default: core }, charts, components, renderers] = await Promise.all([
        import('echarts/core'),
        import('echarts/charts'),
        import('echarts/components'),
        import('echarts/renderers'),
      ])
      core.use([
        charts.BarChart,
        charts.LineChart,
        charts.PieChart,
        charts.ScatterChart,
        components.GridComponent,
        components.TooltipComponent,
        components.LegendComponent,
        components.TitleComponent,
        components.DataZoomComponent,
        components.MarkLineComponent,
        components.MarkPointComponent,
        components.ToolboxComponent,
        renderers.CanvasRenderer,
      ])
      return core
    })()
  }
  return echartsPromise
}

// 已渲染实例与容器的映射，用于防重复渲染；resize 由 ResizeObserver 自持
const chartInstances = new WeakMap<HTMLElement, EChartsType.ECharts>()

/**
 * 解析 ```echarts 代码块里的 option JSON。模型输出常见的小毛病（尾随
 * 逗号）做容错，尽量渲染而不是报错。
 */
export function parseEchartsOption(code: string): Record<string, unknown> | null {
  const text = (code || '').trim()
  if (!text) return null
  const tryParse = (raw: string): Record<string, unknown> | null => {
    try {
      const parsed = JSON.parse(raw)
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>
      }
      return null
    } catch {
      return null
    }
  }
  return tryParse(text) ?? tryParse(text.replace(/,\s*([}\]])/g, '$1'))
}

/**
 * 渲染容器内所有未渲染的 echarts 块（data-echarts="false"）。JSON 非法
 * 时标记 error 并保留原文，方便用户看到模型的原始输出排查。
 */
export const renderEchartsInContainer = async (
  rootElement: HTMLElement | null | undefined,
): Promise<void> => {
  if (!rootElement) return

  const targets = rootElement.querySelectorAll<HTMLElement>(
    '.chat-echarts-block__canvas[data-echarts="false"]',
  )
  if (!targets.length) return
  const echarts = await getEcharts()

  for (const el of targets) {
    if (chartInstances.has(el)) continue

    const code = el.innerText
    const option = parseEchartsOption(code)
    if (!option) {
      el.setAttribute('data-echarts', 'error')
      el.classList.add('chat-echarts-block__canvas--error')
      el.innerHTML = `<div class="chart-render-error">图表 JSON 无法解析，已保留原始输出</div>` +
        escapeHtml(code)
      continue
    }

    try {
      const chart = echarts.init(el, undefined, { renderer: 'canvas' })
      chart.setOption({
        backgroundColor: 'transparent',
        textStyle: { fontFamily: 'PingFang SC, Microsoft YaHei, sans-serif' },
        ...option,
      } as EChartsType.EChartsCoreOption)
      chartInstances.set(el, chart)
      el.setAttribute('data-echarts', 'true')

      const observer = new ResizeObserver(() => chart.resize())
      observer.observe(el)
    } catch (e) {
      console.error('ECharts rendering error:', e)
      el.setAttribute('data-echarts', 'error')
      el.classList.add('chat-echarts-block__canvas--error')
      const reason = e instanceof Error ? e.message : String(e)
      el.innerHTML = `<div class="chart-render-error">图表渲染失败：${escapeHtml(reason.slice(0, 300))}</div>`
    }
  }
}

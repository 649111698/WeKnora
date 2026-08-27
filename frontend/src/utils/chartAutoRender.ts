import { renderMermaidInContainer } from '@/utils/mermaidShared'
import { renderEchartsInContainer } from '@/utils/echartsShared'

/*
 * 全局图表自动渲染兜底。
 *
 * 图表块的正常渲染依赖各消息组件在正确时机调用 markdown 增强
 * （打字机收尾、事件流更新等）。切换会话等场景下这些时机可能全部
 * 错过（组件复用、打字机不重播、watcher 无跳变），任何单一钩子都
 * 无法覆盖所有时序。这里不再依赖时机：常驻观察 document.body，只要
 * 未渲染的图表块（data-mermaid/echarts="false"）出现在 DOM 里，就
 * 防抖调度一次渲染——无论块由哪条路径、在哪个时刻插入。
 */

const PENDING_SELECTOR =
  'pre[data-mermaid="false"], .chat-mermaid-block__canvas[data-mermaid="false"], .chat-echarts-block__canvas[data-echarts="false"]'

let observer: MutationObserver | null = null
let renderTimer: ReturnType<typeof setTimeout> | null = null

// 诊断入口：任何渲染阶段的异常都记录在此，用户控制台一键可读
(window as any).__wkChartErrors = [] as string[]

export function startChartAutoRenderer(): void {
  if (observer || typeof MutationObserver === 'undefined') return
  observer = new MutationObserver(() => {
    if (!document.body?.querySelector(PENDING_SELECTOR)) return
    if (renderTimer) return
    renderTimer = setTimeout(() => {
      renderTimer = null
      void (async () => {
        try {
          await renderMermaidInContainer(document.body)
        } catch (e) {
          noteChartError('mermaid', e)
        }
        try {
          await renderEchartsInContainer(document.body)
        } catch (e) {
          noteChartError('echarts', e)
        }
      })()
    }, 200)
  })
  observer.observe(document.body, { childList: true, subtree: true })
}

function noteChartError(kind: string, e: unknown) {
  const msg = e instanceof Error ? `${e.message}\n${e.stack?.slice(0, 500) ?? ''}` : String(e)
  const entry = `[${new Date().toISOString()}] ${kind}: ${msg}`
  ;(window as any).__wkChartErrors.push(entry)
  console.error('[chart-render]', entry)
}

/**
 * AI 回答截图导出（botmsg / AgentStreamDisplay 共用）。
 *
 * 桌面端直接触发 <a download> 下载；触屏/移动端浏览器（尤其微信、企微
 * 内置浏览器）普遍拦截 data: 链接下载，改为弹出预览层让用户长按保存。
 * 字体嵌入是 html-to-image 最常见的失败源，失败时自动以 skipFonts 降级重试。
 */
import { toCanvas } from 'html-to-image'
import { MessagePlugin } from 'tdesign-vue-next'
import i18n from '@/i18n'
import { get } from '@/utils/request'
import { useAuthStore } from '@/stores/auth'
import { isMostlyUniformRowData, planPageCuts } from '@/utils/answerPdfPagination'

// 导出图四周留白（CSS 像素），截图与 PDF 共用
const EXPORT_PAD = 24

// 预览层样式（纯 DOM 实现，一次性注入）
let previewStylesInjected = false
function injectPreviewStyles() {
  if (previewStylesInjected) return
  previewStylesInjected = true
  const style = document.createElement('style')
  style.textContent = `
.answer-screenshot-preview{position:fixed;inset:0;z-index:12000;background:rgba(0,0,0,.78);display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px;padding:24px;box-sizing:border-box}
.answer-screenshot-preview img{max-width:92vw;max-height:72vh;border-radius:8px;box-shadow:0 8px 32px rgba(0,0,0,.4);object-fit:contain}
.answer-screenshot-preview__tip{color:rgba(255,255,255,.85);font-size:13px;background:rgba(255,255,255,.12);padding:6px 14px;border-radius:16px}
.answer-screenshot-preview__close{position:absolute;top:16px;right:18px;color:#fff;font-size:26px;line-height:1;cursor:pointer;opacity:.8;padding:6px}
`
  document.head.appendChild(style)
}

// 导出水印：页面水印(SiteWatermark)是盖在全站上的独立图层，不在被导出
// 的回答节点内，截图/PDF 天然不带。这里把租户水印文案取来直接绘制到
// 导出图上（截图与 PDF 共用 renderNodeToPng，一处生效）。失败/未开启时
// 不打断导出。
let exportWatermarkCache: string | null | undefined
async function getExportWatermarkText(): Promise<string | null> {
  if (exportWatermarkCache !== undefined) return exportWatermarkCache
  exportWatermarkCache = null
  try {
    const resp: any = await get('/api/v1/tenants/kv/watermark-config')
    if (resp?.data?.enabled) {
      let text = String(resp.data.text || '{username}')
      try {
        text = text.replaceAll('{username}', useAuthStore().user?.username || '')
      } catch {
        // pinia 未就绪等场景保留原文
      }
      if (text.trim()) exportWatermarkCache = text
    }
  } catch {
    // 未登录/接口失败：无水印导出
  }
  return exportWatermarkCache
}

// 与页面水印同款式样（双排错位 -20° 平铺）。导出图会经历 JPEG 压缩与
// 缩放，透明度略高于页面水印（0.06/0.09）以保证压缩后仍可见。
function stampWatermark(canvas: HTMLCanvasElement, text: string, dark: boolean, cssWidth: number) {
  const ctx = canvas.getContext('2d')
  if (!ctx || !cssWidth) return
  const s = canvas.width / cssWidth // canvas 像素相对 CSS 像素的倍率
  const font = 14
  const tileW = Math.max(180, text.length * font + 90) * s
  const tileH = 110 * s
  ctx.fillStyle = dark ? 'rgba(255, 255, 255, 0.13)' : 'rgba(0, 0, 0, 0.09)'
  ctx.font = `${Math.round(font * s)}px sans-serif`
  ctx.textAlign = 'center'
  ctx.textBaseline = 'middle'
  const rad = (-20 * Math.PI) / 180
  const drawOne = (x: number, y: number) => {
    ctx.save()
    ctx.translate(x, y)
    ctx.rotate(rad)
    ctx.fillText(text, 0, 0)
    ctx.restore()
  }
  for (let y = 0; y < canvas.height + tileH; y += tileH) {
    for (let x = 0; x < canvas.width + tileW; x += tileW) {
      drawOne(x + tileW / 2, y + tileH * 0.3)
      drawOne(x + tileW / 4, y + tileH * 0.85)
    }
  }
}

function pageBackground(): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue('--td-bg-color-container').trim()
  return v || '#ffffff'
}

function isTouchDevice(): boolean {
  return (
    (typeof window !== 'undefined' && 'ontouchstart' in window) ||
    (typeof window !== 'undefined' && window.matchMedia?.('(pointer: coarse)').matches)
  )
}

let previewOverlay: HTMLDivElement | null = null

function closeScreenshotPreview() {
  previewOverlay?.remove()
  previewOverlay = null
  document.documentElement.style.removeProperty('overflow')
}

function showScreenshotPreview(dataUrl: string) {
  injectPreviewStyles()
  closeScreenshotPreview()
  const overlay = document.createElement('div')
  overlay.className = 'answer-screenshot-preview'
  overlay.addEventListener('click', closeScreenshotPreview)

  const img = document.createElement('img')
  img.src = dataUrl
  img.addEventListener('click', (e) => e.stopPropagation())

  const tip = document.createElement('div')
  tip.className = 'answer-screenshot-preview__tip'
  tip.textContent = i18n.global.t('chat.screenshotLongPress') as string
  tip.addEventListener('click', (e) => e.stopPropagation())

  const close = document.createElement('div')
  close.className = 'answer-screenshot-preview__close'
  close.textContent = '×'
  close.addEventListener('click', closeScreenshotPreview)

  overlay.append(img, tip, close)
  document.documentElement.style.setProperty('overflow', 'hidden', 'important')
  document.body.appendChild(overlay)
  previewOverlay = overlay
}

// 导出文件名时间戳：用户可读的 yyyymmdd-HHmmss（中文文件名 + 短横线，
// 无平台前缀）。
function formatExportStamp(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`
}

function triggerDownload(dataUrl: string) {
  const link = document.createElement('a')
  link.href = dataUrl
  link.download = `回答导出-${formatExportStamp()}.png`
  link.click()
}

// 导出前强制加载节点内的所有图片：移动端懒加载或尚未滚入视口的图片
// 可能从未加载，html-to-image 克隆 DOM 时只能拿到空位（表现为导出的
// PDF/截图里图片消失）。等待 load/error，单图 5s 兜底防卡死导出。
async function preloadNodeImages(node: HTMLElement): Promise<void> {
  const imgs = Array.from(node.querySelectorAll('img'));
  if (!imgs.length) return;
  await Promise.all(
    imgs.map(img => {
      if (img.complete && img.naturalWidth > 0) return Promise.resolve();
      if (img.getAttribute('loading') === 'lazy') {
        img.setAttribute('loading', 'eager');
      }
      return new Promise<void>(resolve => {
        let settled = false;
        const done = () => {
          if (settled) return;
          settled = true;
          resolve();
        };
        img.addEventListener('load', done, { once: true });
        img.addEventListener('error', done, { once: true });
        setTimeout(done, 5000);
        // 重赋 src 触发个别浏览器改 loading 属性后不自动重试的加载
        if (img.src) {
          const src = img.src;
          img.src = src;
        }
      });
    })
  );
  // 等待解码，避免 canvas 绘制时图像尚未就绪
  await Promise.all(
    imgs.map(img =>
      img.complete && img.naturalWidth > 0
        ? img.decode?.().catch(() => undefined)
        : Promise.resolve()
    )
  );
}

// 导出展开样式（一次性注入）：会话内的表格与图表默认限高（520px/65vh）
// 并在盒内滚动，html-to-image 截取的是实时 DOM，会把滚动条和被裁掉的
// 内容一并截进图片/PDF。导出瞬间解除限高与滚动，让内容完整铺开。
let exportExpansionStylesInjected = false
function injectExportExpansionStyles() {
  if (exportExpansionStylesInjected) return
  exportExpansionStylesInjected = true
  const style = document.createElement('style')
  style.textContent = `
.chat-export-mode .chat-markdown-table{max-height:none!important;max-width:none!important;overflow:visible!important}
.chat-export-mode .chat-mermaid-block__canvas,.chat-export-mode pre.mermaid{max-height:none!important;overflow:visible!important}
`
  document.head.appendChild(style)
}

// 进入导出模式并返回恢复函数。解除滚动盒限制后，横向超宽的表格会
// 溢出到回答节点之外，此时按内容把节点临时撑宽，避免截图右缘裁切。
function expandScrollContainersForExport(node: HTMLElement): () => void {
  injectExportExpansionStyles()
  node.classList.add('chat-export-mode')
  const prevInlineWidth = node.style.width
  // 读 offsetWidth 触发重排，确保下面的测量基于展开后的布局。
  const baseWidth = node.offsetWidth
  const nodeLeft = node.getBoundingClientRect().left
  let extra = 0
  node
    .querySelectorAll<HTMLElement>('.chat-markdown-table, .chat-mermaid-block__canvas, pre.mermaid')
    .forEach((el) => {
      extra = Math.max(extra, el.getBoundingClientRect().right - nodeLeft - baseWidth)
    })
  if (extra > 1) node.style.width = `${baseWidth + Math.ceil(extra)}px`
  return () => {
    node.classList.remove('chat-export-mode')
    node.style.width = prevInlineWidth
  }
}

// 渲染节点为 canvas（截图与 PDF 导出共用；字体嵌入失败时降级重试）。
// 返回的是未盖水印的原始图，水印由各导出路径自行叠加。
async function renderNodeToCanvas(node: HTMLElement): Promise<HTMLCanvasElement | null> {
  await preloadNodeImages(node);
  // html-to-image 克隆时逐元素拷贝实时计算样式，节点必须在捕获期间
  // 保持展开状态，恢复放在捕获结束后。
  const restoreExportLayout = expandScrollContainersForExport(node)
  const options = {
    backgroundColor: pageBackground(),
    pixelRatio: 2,
    // 不开 cacheBust：它会给资源 URL 追加查询参数，回答内联的上传图片
    // 是 blob: URL，加参数后变成非法地址加载失败，整个导出随之报错。
    style: { padding: `${EXPORT_PAD}px` },
    width: node.offsetWidth + EXPORT_PAD * 2,
    height: node.offsetHeight + EXPORT_PAD * 2,
  }
  try {
    return await toCanvas(node, options)
  } catch (error) {
    console.error('[answerExport] render failed, retrying without font embedding:', error)
    try {
      return await toCanvas(node, { ...options, skipFonts: true })
    } catch (retryError) {
      console.error('[answerExport] render failed:', retryError)
      return null
    }
  } finally {
    restoreExportLayout()
  }
}

// 渲染节点为带留白的 PNG（字体嵌入失败时降级重试），
// 末尾按租户配置叠加全图水印
async function renderNodeToPng(node: HTMLElement): Promise<string | null> {
  const canvas = await renderNodeToCanvas(node)
  if (!canvas) return null

  const watermarkText = await getExportWatermarkText()
  if (watermarkText) {
    try {
      const dark = document.documentElement.getAttribute('theme-mode') === 'dark'
      stampWatermark(canvas, watermarkText, dark, node.offsetWidth + EXPORT_PAD * 2)
    } catch (error) {
      console.warn('[answerExport] watermark stamp failed, exporting without it:', error)
    }
  }
  return canvas.toDataURL('image/png')
}

export async function exportAnswerScreenshot(node: HTMLElement): Promise<void> {
  const t = i18n.global.t
  const dataUrl = await renderNodeToPng(node)
  if (!dataUrl || !dataUrl.startsWith('data:image/png')) {
    MessagePlugin.error(t('chat.screenshotFailed') as string)
    return
  }

  if (isTouchDevice()) {
    showScreenshotPreview(dataUrl)
  } else {
    triggerDownload(dataUrl)
  }
  MessagePlugin.success(t('chat.screenshotSuccess') as string)
}

/**
 * 导出为 A4 分页 PDF：复用 PNG 渲染管线（同主题背景/留白），
 * 按 A4 内容区切片逐页写入 jsPDF。blob: 下载在移动端浏览器兼容性
 * 远好于 data:（微信内核除外，那里可用截图预览替代）。
 */

// 读取源图某一像素行，判定其是否可作为切页点：基本同色（允许表格行
// 间隙里的竖向细边框线离群），有文字/图形则不可切。
function makeSafeRowProbe(ctx: CanvasRenderingContext2D, width: number): (y: number) => boolean {
  return (y: number) => isMostlyUniformRowData(ctx.getImageData(0, y, width, 1).data)
}

export async function exportAnswerPDF(node: HTMLElement): Promise<void> {
  const t = i18n.global.t
  const canvas = await renderNodeToCanvas(node)
  if (!canvas) {
    MessagePlugin.error(t('chat.exportPdfFailed') as string)
    return
  }

  try {
    const { jsPDF } = await import('jspdf')

    const pdf = new jsPDF({ unit: 'pt', format: 'a4', compress: true })
    const pageW = pdf.internal.pageSize.getWidth()
    const pageH = pdf.internal.pageSize.getHeight()
    const margin = 24
    const contentW = pageW - margin * 2
    const contentH = pageH - margin * 2
    const scale = contentW / canvas.width
    const imgH = canvas.height
    const bg = pageBackground()
    const cssWidth = node.offsetWidth + EXPORT_PAD * 2

    const watermarkText = await getExportWatermarkText()
    const dark = document.documentElement.getAttribute('theme-mode') === 'dark'

    // 智能分页：切点优先落在整行同色的像素上（行间/段落间隙），避免把
    // 文字行切成上下两半；超过一页仍找不到安全切点（超长图片/代码块）
    // 才退回硬切。算法与容差见 answerPdfPagination.ts。
    const src = canvas.getContext('2d')
    const pageSrcH = contentH / scale
    const cuts = src
      ? planPageCuts(
          imgH,
          pageSrcH,
          {
            // 回扫窗口放大到 65%：饼图/流程图这类整块内容常高 300-500px，
            // 原窗口（22%）找不到安全切点会硬切图表；放大后切点能移到
            // 图表上方。minAdvance(30%) 仍保证每页最低内容量。
            lookback: Math.floor(pageSrcH * 0.65),
            minAdvance: Math.max(8, Math.floor(pageSrcH * 0.3)),
            band: 2,
          },
          makeSafeRowProbe(src, canvas.width)
        )
      : [imgH]

    let prev = 0
    for (let i = 0; i < cuts.length; i++) {
      const cut = cuts[i]
      if (i > 0) pdf.addPage()
      const sliceH = (cut - prev) * scale

      const slice = document.createElement('canvas')
      slice.width = canvas.width
      slice.height = Math.max(1, Math.round(cut - prev))
      const ctx = slice.getContext('2d')
      if (!ctx) break
      ctx.fillStyle = bg
      ctx.fillRect(0, 0, slice.width, slice.height)
      ctx.drawImage(canvas, 0, prev, canvas.width, cut - prev, 0, 0, canvas.width, cut - prev)
      // 每页单独盖水印：整图盖完再切会把水印文字一并切开，且各页平铺
      // 位置一致，观感与页面水印更接近
      if (watermarkText) {
        try {
          stampWatermark(slice, watermarkText, dark, cssWidth)
        } catch (error) {
          console.warn('[answerExport] watermark stamp failed on page, skipping:', error)
        }
      }
      // JPEG 而非 PNG：带图回答的 PNG 每页可到数 MB，接收方（微信预览等）
      // 常打不开；JPEG 体积小一个数量级，白底文本在 0.92 质量下无可见损失
      pdf.addImage(slice.toDataURL('image/jpeg', 0.92), 'JPEG', margin, margin, contentW, sliceH)
      prev = cut
    }

    const fileName = `回答导出-${formatExportStamp()}.pdf`
    // 移动端优先走系统分享：jsPDF 的 a[download]+blob 在企微/微信内置浏览器
    // 只会打开一个临时预览，用户"看到了"但从未真正落盘，转发出去的自然
    // 不是文件本体（接收方打不开）。Web Share API 直接以 PDF 文件唤起
    // 分享面板，转发出去的是真实文件；不支持时回退原有保存行为。
    if (isTouchDevice() && typeof navigator.share === 'function' && typeof navigator.canShare === 'function') {
      try {
        const file = new File([pdf.output('blob')], fileName, { type: 'application/pdf' })
        if (navigator.canShare({ files: [file] })) {
          await navigator.share({ files: [file], title: fileName })
          MessagePlugin.success(t('chat.exportPdfSuccess') as string)
          return
        }
      } catch (error: any) {
        // AbortError = 用户收起分享面板，视为完成；其余错误回退到保存
        if (error?.name === 'AbortError') {
          return
        }
        console.warn('[answerExport] navigator.share failed, falling back to save:', error)
      }
    }
    pdf.save(fileName)
    MessagePlugin.success(t('chat.exportPdfSuccess') as string)
  } catch (error) {
    console.error('[answerExport] PDF export failed:', error)
    MessagePlugin.error(t('chat.exportPdfFailed') as string)
  }
}

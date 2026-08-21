/**
 * AI 回答截图导出（botmsg / AgentStreamDisplay 共用）。
 *
 * 桌面端直接触发 <a download> 下载；触屏/移动端浏览器（尤其微信、企微
 * 内置浏览器）普遍拦截 data: 链接下载，改为弹出预览层让用户长按保存。
 * 字体嵌入是 html-to-image 最常见的失败源，失败时自动以 skipFonts 降级重试。
 */
import { toPng } from 'html-to-image'
import { MessagePlugin } from 'tdesign-vue-next'
import i18n from '@/i18n'

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

function triggerDownload(dataUrl: string) {
  const link = document.createElement('a')
  link.href = dataUrl
  link.download = `weknora-answer-${Date.now()}.png`
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

// 渲染节点为带留白的 PNG（截图与 PDF 导出共用；字体嵌入失败时降级重试）
async function renderNodeToPng(node: HTMLElement): Promise<string | null> {
  await preloadNodeImages(node);
  const PAD = 24
  const options = {
    backgroundColor: pageBackground(),
    pixelRatio: 2,
    // 不开 cacheBust：它会给资源 URL 追加查询参数，回答内联的上传图片
    // 是 blob: URL，加参数后变成非法地址加载失败，整个导出随之报错。
    style: { padding: `${PAD}px` },
    width: node.offsetWidth + PAD * 2,
    height: node.offsetHeight + PAD * 2,
  }
  try {
    return await toPng(node, options)
  } catch (error) {
    console.error('[answerExport] render failed, retrying without font embedding:', error)
    try {
      return await toPng(node, { ...options, skipFonts: true })
    } catch (retryError) {
      console.error('[answerExport] render failed:', retryError)
      return null
    }
  }
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
export async function exportAnswerPDF(node: HTMLElement): Promise<void> {
  const t = i18n.global.t
  const dataUrl = await renderNodeToPng(node)
  if (!dataUrl || !dataUrl.startsWith('data:image/png')) {
    MessagePlugin.error(t('chat.exportPdfFailed') as string)
    return
  }

  try {
    const { jsPDF } = await import('jspdf')
    const img = await new Promise<HTMLImageElement>((resolve, reject) => {
      const el = new Image()
      el.onload = () => resolve(el)
      el.onerror = reject
      el.src = dataUrl
    })

    const pdf = new jsPDF({ unit: 'pt', format: 'a4', compress: true })
    const pageW = pdf.internal.pageSize.getWidth()
    const pageH = pdf.internal.pageSize.getHeight()
    const margin = 24
    const contentW = pageW - margin * 2
    const contentH = pageH - margin * 2
    const scale = contentW / img.width
    const fullH = img.height * scale
    const pages = Math.max(1, Math.ceil(fullH / contentH))
    const bg = pageBackground()

    for (let i = 0; i < pages; i++) {
      if (i > 0) pdf.addPage()
      const sliceH = Math.min(contentH, fullH - i * contentH)
      const srcY = (i * contentH) / scale
      const srcH = sliceH / scale
      const canvas = document.createElement('canvas')
      canvas.width = img.width
      canvas.height = Math.max(1, Math.round(srcH))
      const ctx = canvas.getContext('2d')
      if (!ctx) break
      ctx.fillStyle = bg
      ctx.fillRect(0, 0, canvas.width, canvas.height)
      ctx.drawImage(img, 0, srcY, img.width, srcH, 0, 0, img.width, srcH)
      // JPEG 而非 PNG：带图回答的 PNG 每页可到数 MB，接收方（微信预览等）
      // 常打不开；JPEG 体积小一个数量级，白底文本在 0.92 质量下无可见损失
      pdf.addImage(canvas.toDataURL('image/jpeg', 0.92), 'JPEG', margin, margin, contentW, sliceH)
    }

    const fileName = `weknora-answer-${Date.now()}.pdf`
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

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

export async function exportAnswerScreenshot(node: HTMLElement): Promise<void> {
  const t = i18n.global.t
  const options = {
    backgroundColor: pageBackground(),
    pixelRatio: 2,
    cacheBust: true,
  } as const

  let dataUrl: string
  try {
    dataUrl = await toPng(node, options)
  } catch (error) {
    console.error('[answerScreenshot] export failed, retrying without font embedding:', error)
    // 字体嵌入失败是最常见的降级场景（跨域字体 / 移动端内置浏览器）
    try {
      dataUrl = await toPng(node, { ...options, skipFonts: true })
    } catch (retryError) {
      console.error('[answerScreenshot] export failed:', retryError)
      MessagePlugin.error(t('chat.screenshotFailed') as string)
      return
    }
  }

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

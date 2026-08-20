<template>
  <!--
    全站水印 overlay：canvas 平铺文本，pointer-events:none 不影响任何交互。
    z-index 覆盖抽屉/弹层（TDesign 弹层 < 3000）。根节点被移除时由
    MutationObserver 自愈重新挂载（防审查元素删除）。
  -->
  <div ref="watermarkRef" class="site-watermark" aria-hidden="true"></div>
</template>

<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'

const props = defineProps<{
  /** 水印文案，调用方已完成 {username} 等占位符替换 */
  text: string
}>()

const watermarkRef = ref<HTMLElement | null>(null)
let resizeObserver: ResizeObserver | null = null
let mutationObserver: MutationObserver | null = null
let themeObserver: MutationObserver | null = null
let canvas: HTMLCanvasElement | null = null

function draw() {
  const host = watermarkRef.value
  if (!host) return
  if (!canvas) {
    canvas = document.createElement('canvas')
    canvas.className = 'site-watermark__canvas'
    host.appendChild(canvas)
  }

  // 深色模式下用浅色水印，否则用深色
  const dark = document.documentElement.getAttribute('theme-mode') === 'dark'
  const color = dark ? 'rgba(255, 255, 255, 0.09)' : 'rgba(0, 0, 0, 0.06)'

  // 单块 tile 尺寸按文案长度自适应，保证平铺密度稳定
  const font = 14
  const tileWidth = Math.max(180, props.text.length * font + 90)
  const tileHeight = 110
  const dpr = Math.min(window.devicePixelRatio || 1, 2)

  canvas.width = tileWidth * dpr
  canvas.height = tileHeight * dpr
  canvas.style.width = `${tileWidth}px`
  canvas.style.height = `${tileHeight}px`

  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.scale(dpr, dpr)
  ctx.clearRect(0, 0, tileWidth, tileHeight)
  ctx.font = `${font}px sans-serif`
  ctx.fillStyle = color
  ctx.textAlign = 'center'
  ctx.textBaseline = 'middle'
  // 双排错位：密度更高，截图外传时更难裁掉
  ctx.save()
  ctx.translate(tileWidth / 2, tileHeight * 0.3)
  ctx.rotate((-20 * Math.PI) / 180)
  ctx.fillText(props.text, 0, 0)
  ctx.restore()
  ctx.save()
  ctx.translate(tileWidth / 4, tileHeight * 0.85)
  ctx.rotate((-20 * Math.PI) / 180)
  ctx.fillText(props.text, 0, 0)
  ctx.restore()

  host.style.backgroundImage = `url(${canvas.toDataURL('image/png')})`
}

// 自愈：节点被删除时重新追加并重绘
function setupSelfHeal() {
  const host = watermarkRef.value
  if (!host || !host.parentNode) return
  mutationObserver = new MutationObserver(() => {
    if (watermarkRef.value && !watermarkRef.value.isConnected && host.parentNode) {
      host.parentNode.appendChild(host)
      draw()
    }
  })
  mutationObserver.observe(host.parentNode, { childList: true })
}

onMounted(() => {
  draw()
  setupSelfHeal()
  resizeObserver = new ResizeObserver(() => draw())
  resizeObserver.observe(document.documentElement)
  // 主题切换（theme-mode 属性）时重绘：明暗模式水印颜色不同
  themeObserver = new MutationObserver(() => draw())
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['theme-mode'] })
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  mutationObserver?.disconnect()
  themeObserver?.disconnect()
})

watch(() => props.text, () => draw())
</script>

<style scoped>
.site-watermark {
  position: fixed;
  inset: 0;
  z-index: 9000;
  pointer-events: none;
  background-repeat: repeat;
  overflow: hidden;
}
</style>

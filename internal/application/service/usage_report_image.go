package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/Tencent/WeKnora/internal/types"
)

// 报告图片渲染：企微 markdown 消息在部分客户端（微信插件等）不渲染，
// 纯文本缺乏层次；把日报绘制成 PNG 通过 media/upload + image 消息发送，
// 所有客户端显示一致。字体取自运行镜像的 fonts-noto-cjk；缺失或渲染
// 失败时回退纯文本消息。
//
// 视觉规范（与前端卡片语言对齐）：浅灰画布 + 白色圆角卡片分区；深色
// 头部横幅承载标题与整体达标率；四个统计瓦片用低饱和语义色底（蓝=总量、
// 绿=达标、橙=未达标、石板=消息）；状态用胶囊徽章；达标率配进度条；
// 全程不用 emoji——CJK 矢量字体不含彩色 emoji，会渲染成方框。

const (
	imgW = 1080
	// 品牌名固定（白标部署统一对外名称，不随租户名变化）。
	reportBrand = "智能知识库"

	pageMargin = 40 // 画布四边
	cardPad    = 42 // 卡片内边距
	cardRadius = 28
	gapModule  = 42  // 模块之间统一大间距（清单三.2）
	gapSmall   = 30  // 模块内次级间距
	tileH      = 172 // 指标瓦片（内部留白放大，清单三.3.1）
	imgRowH    = 66  // 表格行高放大（清单三.3.2）
	headRowH   = 62
	imgMaxRows = 30
)

type reportColor struct{ r, g, b uint8 }

func (c reportColor) rgba() color.RGBA { return color.RGBA{c.r, c.g, c.b, 255} }

// 色板对齐日报模板（智能知识库-日报模板.html）。
var (
	icCanvas      = reportColor{0xF5, 0xF7, 0xFC} // 页面底
	icCard        = reportColor{0xFF, 0xFF, 0xFF} // 卡片底
	icInk         = reportColor{0x0B, 0x1B, 0x3A} // 主文字
	icInk2        = reportColor{0x1D, 0x2B, 0x41} // 表格正文
	icMuted       = reportColor{0x6B, 0x7A, 0x8F} // 日期/页脚
	icLabel       = reportColor{0x5E, 0x6F, 0x88} // 指标 label
	icBlue        = reportColor{0x1A, 0x4C, 0xFF} // 品牌蓝
	icBlueBg      = reportColor{0xF0, 0xF4, 0xFE}
	icBlueChip    = reportColor{0xD6, 0xE4, 0xFF}
	icTileBg      = reportColor{0xF8, 0xFA, 0xFF}
	icTileBorder  = reportColor{0xEE, 0xF3, 0xFA}
	icGreen       = reportColor{0x0F, 0x7B, 0x3A}
	icGreenBg     = reportColor{0xE1, 0xF7, 0xE8}
	icRed         = reportColor{0xE8, 0x54, 0x4A}
	icRedBadge    = reportColor{0xBC, 0x2E, 0x26}
	icRedBg       = reportColor{0xFF, 0xE9, 0xE7}
	icSep         = reportColor{0xEE, 0xF2, 0xF6}
	icRowSep      = reportColor{0xED, 0xF2, 0xF8}
	icThBg        = reportColor{0xF9, 0xFC, 0xFF}
	icThText      = reportColor{0x3A, 0x4C, 0x66}
	icLastActive  = reportColor{0x4E, 0x62, 0x7C}
	icGreetBg     = reportColor{0xF8, 0xFB, 0xFF}
	icGreetBorder = reportColor{0xEA, 0xF0, 0xF8}
	icRuleBg      = reportColor{0xEE, 0xF4, 0xFE}
	icRuleBorder  = reportColor{0xDC, 0xE7, 0xFC}
	icMsgBg       = reportColor{0xF2, 0xF7, 0xFF}
	icTrendBg     = reportColor{0xEE, 0xF5, 0xFE}
	icCompareBg   = reportColor{0xEA, 0xF1, 0xFA}
	icFooterGray  = reportColor{0x8A, 0x9A, 0xA8}
)

// --- 字体（进程内一次性加载，面按需创建） ---

var (
	reportFontsOnce sync.Once
	reportFontReg   *opentype.Font
	reportFontBold  *opentype.Font
	reportFontsErr  error
)

// findNotoCJK 定位 CJK 字体；WEKNORA_REPORT_FONT 可指向宿主机上的任意
// ttc/ttf（本地预览/测试用），生产镜像走默认搜索路径。
func findNotoCJK(preferBold bool) string {
	ordered := []string{"NotoSansCJK-Regular.ttc", "NotoSansCJK-Bold.ttc"}
	if preferBold {
		ordered = []string{"NotoSansCJK-Bold.ttc", "NotoSansCJK-Regular.ttc"}
	}
	if p := strings.TrimSpace(os.Getenv("WEKNORA_REPORT_FONT")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	roots := []string{
		"/usr/share/fonts/opentype/noto",
		"/usr/share/fonts/truetype/noto",
		"/usr/share/fonts",
	}
	for _, root := range roots {
		for _, name := range ordered {
			p := filepath.Join(root, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// pickCJKFont 解析 ttc，取第一个能渲染汉字的字重面所在字体。
func pickCJKFont(path string) (*opentype.Font, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	col, err := opentype.ParseCollection(data)
	if err != nil {
		return nil, err
	}
	for i := 0; i < col.NumFonts(); i++ {
		f, ferr := col.Font(i)
		if ferr != nil {
			continue
		}
		face, ferr := opentype.NewFace(f, &opentype.FaceOptions{Size: 30, DPI: 96})
		if ferr != nil {
			continue
		}
		_, ok := face.GlyphAdvance('达')
		_ = face.Close()
		if ok {
			return f, nil
		}
	}
	return nil, fmt.Errorf("no face in %s renders CJK", path)
}

func loadReportFonts() (*opentype.Font, *opentype.Font, error) {
	regPath := findNotoCJK(false)
	if regPath == "" {
		return nil, nil, fmt.Errorf("no CJK font found (install fonts-noto-cjk)")
	}
	reg, err := pickCJKFont(regPath)
	if err != nil {
		return nil, nil, err
	}
	boldFont := reg
	if boldPath := findNotoCJK(true); boldPath != "" && boldPath != regPath {
		if b, err := pickCJKFont(boldPath); err == nil {
			boldFont = b
		}
	}
	return reg, boldFont, nil
}

// reportCanvas 一次绘制的上下文：持有图片与按字号缓存的字体面。
type reportCanvas struct {
	img   *image.RGBA
	reg   *opentype.Font
	bold  *opentype.Font
	faces map[string]font.Face
}

func newReportCanvas(height int) (*reportCanvas, error) {
	reportFontsOnce.Do(func() {
		reportFontReg, reportFontBold, reportFontsErr = loadReportFonts()
	})
	if reportFontsErr != nil {
		return nil, reportFontsErr
	}
	return &reportCanvas{
		img:   image.NewRGBA(image.Rect(0, 0, imgW, height)),
		reg:   reportFontReg,
		bold:  reportFontBold,
		faces: map[string]font.Face{},
	}, nil
}

func (c *reportCanvas) close() {
	for _, f := range c.faces {
		_ = f.Close()
	}
}

func (c *reportCanvas) face(size float64, bold bool) (font.Face, error) {
	key := fmt.Sprintf("%v-%v", size, bold)
	if f, ok := c.faces[key]; ok {
		return f, nil
	}
	src := c.reg
	if bold {
		src = c.bold
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{Size: size, DPI: 96})
	if err != nil {
		return nil, err
	}
	c.faces[key] = f
	return f, nil
}

func (c *reportCanvas) textWidth(s string, size float64, bold bool) int {
	f, err := c.face(size, bold)
	if err != nil {
		return len(s) * int(size)
	}
	d := &font.Drawer{Face: f}
	return d.MeasureString(s).Ceil()
}

// text 在基线 (x, y) 左对齐绘制。
func (c *reportCanvas) text(x, y int, s string, size float64, bold bool, col reportColor) {
	f, err := c.face(size, bold)
	if err != nil {
		return
	}
	d := &font.Drawer{
		Dst:  c.img,
		Src:  image.NewUniform(col.rgba()),
		Face: f,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

func (c *reportCanvas) textCenter(cx, y int, s string, size float64, bold bool, col reportColor) {
	w := c.textWidth(s, size, bold)
	c.text(cx-w/2, y, s, size, bold, col)
}

func (c *reportCanvas) textRight(rx, y int, s string, size float64, bold bool, col reportColor) {
	w := c.textWidth(s, size, bold)
	c.text(rx-w, y, s, size, bold, col)
}

// truncate 按可用宽度截断并加省略号。
func (c *reportCanvas) truncate(s string, maxW int, size float64, bold bool) string {
	if c.textWidth(s, size, bold) <= maxW {
		return s
	}
	for len(s) > 1 && c.textWidth(s+"…", size, bold) > maxW {
		r := []rune(s)
		s = string(r[:len(r)-1])
	}
	return s + "…"
}

// truncateMiddle 长用户名保留头尾、中间省略，域名后缀等信息不丢。
func (c *reportCanvas) truncateMiddle(s string, maxW int, size float64, bold bool) string {
	if c.textWidth(s, size, bold) <= maxW {
		return s
	}
	r := []rune(s)
	head := len(r) * 3 / 5
	tail := len(r) / 4
	for head > 1 && c.textWidth(string(r[:head])+"…"+string(r[len(r)-tail:]), size, bold) > maxW {
		head--
		if tail > 1 && c.textWidth(string(r[:head])+"…"+string(r[len(r)-tail:]), size, bold) > maxW {
			tail--
		}
	}
	if head <= 1 {
		return c.truncate(s, maxW, size, bold)
	}
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

func (c *reportCanvas) fillRect(x, y, w, h int, col reportColor) {
	if w <= 0 || h <= 0 {
		return
	}
	draw.Draw(c.img, image.Rect(x, y, x+w, y+h), &image.Uniform{col.rgba()}, image.Point{}, draw.Src)
}

// fillRoundRect 圆角矩形：三块矩形拼腰身 + 四角逐行扫描四分之一圆。
func (c *reportCanvas) fillRoundRect(x, y, w, h, r int, col reportColor) {
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	c.fillRect(x+r, y, w-2*r, h, col)
	c.fillRect(x, y+r, w, h-2*r, col)
	for j := 0; j < r; j++ {
		ch := int(math.Sqrt(float64(r*r - j*j)))
		if ch <= 0 {
			continue
		}
		c.fillRect(x+r-ch, y+j, ch, 1, col)     // 左上
		c.fillRect(x+w-r, y+j, ch, 1, col)      // 右上
		c.fillRect(x+r-ch, y+h-1-j, ch, 1, col) // 左下
		c.fillRect(x+w-r, y+h-1-j, ch, 1, col)  // 右下
	}
}

func (c *reportCanvas) hline(x1, x2, y int, col reportColor) {
	c.fillRect(x1, y, x2-x1, 1, col)
}

// pill 胶囊徽章：低饱和底 + 语义色文字（居中）。
func (c *reportCanvas) pill(cx, cy int, label string, bg, fg reportColor, size float64) {
	tw := c.textWidth(label, size, true)
	pw := tw + 44
	ph := 40
	c.fillRoundRect(cx-pw/2, cy-ph/2, pw, ph, ph/2, bg)
	c.textCenter(cx, cy+int(size*0.38), label, size, true, fg)
}

// reportDisplayName 明细表里去掉 SSO 平台前缀，长账号更可读（姓名优
// 先展示，这里只是无姓名时的账号兜底）。
func reportDisplayName(u string) string {
	for _, p := range []string{"wecom_", "feishu_", "kingdee_", "oidc_"} {
		u = strings.TrimPrefix(u, p)
	}
	return u
}

// borderedPill 带描边的浅色胶囊（考核标准等）。
func (c *reportCanvas) borderedPill(x, y, h int, label string, bg, border, fg reportColor) int {
	tw := c.textWidth(label, 24, false)
	pw := tw + 56
	c.fillRoundRect(x, y, pw, h, h/2, border)
	c.fillRoundRect(x+1, y+1, pw-2, h-2, h/2-1, bg)
	c.text(x+28, y+h/2+9, label, 24, false, fg)
	return pw
}

// statTile 居中指标瓦片：label / 大数字 / sub 三段拉开上下距离
// （清单五.2：避免文字堆叠），数字保留字重、label/sub 常规。
func (c *reportCanvas) statTile(x, y, w int, valueCol reportColor, label, value, sub string) {
	c.fillRoundRect(x, y, w, tileH, 18, icTileBg)
	c.fillRect(x+18, y, w-36, 1, icTileBorder)
	c.fillRect(x+18, y+tileH-1, w-36, 1, icTileBorder)
	lw := c.textWidth(label, 23, false)
	c.text(x+(w-lw)/2, y+54, label, 23, false, icLabel)
	vw := c.textWidth(value, 54, true)
	c.text(x+(w-vw)/2, y+118, value, 54, true, valueCol)
	sw := c.textWidth(sub, 22, false)
	c.text(x+(w-sw)/2, y+152, sub, 22, false, icLabel)
}

// renderUsageReportImage 绘制整份日报为 PNG。版式对齐前端模板与样式
// 调整清单：白色大卡片，模块间用大段留白分隔（不依赖重色块）；正文
// 全部常规字重，仅标题/表头/指标数字/状态徽章保留字重；文案统一用
// 固定品牌名。模板中的 emoji 在 CJK 矢量字体下渲染为方框，全部省略。
func renderUsageReportImage(r *UsageReport, now time.Time) ([]byte, error) {
	rows := r.Rows
	hidden := 0
	if len(rows) > imgMaxRows {
		hidden = len(rows) - imgMaxRows
		rows = rows[:imgMaxRows]
	}
	activeUsers := 0
	for _, row := range r.Rows {
		if row.Logins >= usageReportLoginMin || row.Chats >= usageReportChatMin {
			activeUsers++
		}
	}

	tableH := headRowH + len(rows)*imgRowH + 12
	if hidden > 0 {
		tableH += 48
	}
	// 顺序排版 + 220px 余量，画完裁剪。
	totalH := pageMargin + 52 + 176 + gapModule + 200 + gapModule + tileH +
		gapSmall + 68 + gapSmall + tableH + gapModule + 210 + 44 + pageMargin + 220

	cv, err := newReportCanvas(totalH)
	if err != nil {
		return nil, err
	}
	defer cv.close()
	draw.Draw(cv.img, cv.img.Bounds(), &image.Uniform{icCanvas.rgba()}, image.Point{}, draw.Src)

	weekday := usageReportWeekday(r.Date, now)
	rate := pct(r.Qualified, r.TotalUsers)
	perm := ratePermille(r.Qualified, r.TotalUsers)
	prevPerm := ratePermille(r.PrevQualified, r.TotalUsers)
	diffPP := float64(perm-prevPerm) / 10.0

	// ===== 白色大卡片 =====
	cx0, cy0 := pageMargin, pageMargin
	cw := imgW - pageMargin*2
	x0 := cx0 + cardPad
	cw2 := cw - cardPad*2
	y := cy0

	// --- 头部：标题 + 日期；右侧“较前日”胶囊（数字保留字重） ---
	cv.text(x0, y+76, reportBrand+" · 使用日报", 40, true, icInk)
	cv.text(x0, y+122, fmt.Sprintf("%s（%s）", r.Date, weekday), 26, false, icMuted)
	deltaTxt := "持平"
	if diffPP > 0 {
		deltaTxt = fmt.Sprintf("+%.1f%%", diffPP)
	} else if diffPP < 0 {
		deltaTxt = fmt.Sprintf("%.1f%%", diffPP)
	}
	chipBg := icBlue
	if diffPP < 0 {
		chipBg = icRed
	}
	chipFg := reportColor{0xFF, 0xFF, 0xFF}
	{
		lbl := "较前日"
		lw := cv.textWidth(lbl, 26, false)
		cw2v := cv.textWidth(deltaTxt, 24, true)
		pw := lw + cw2v + 78
		ph := 58
		px := x0 + cw2 - pw
		py := y + 42
		cv.fillRoundRect(px, py, pw, ph, ph/2, icBlueBg)
		cv.text(px+28, py+ph/2+9, lbl, 26, false, icBlue)
		cv.fillRoundRect(px+28+lw+12, py+13, cw2v+28, ph-26, (ph-26)/2, chipBg)
		cv.text(px+28+lw+12+14, py+ph/2+8, deltaTxt, 24, true, chipFg)
	}
	cv.fillRect(x0, y+148, cw2, 1, icSep)
	y += 176

	// --- 问候区：两行问候 + 考核标准胶囊，行距放宽（清单三.1） ---
	greetH := 200
	cv.fillRoundRect(x0, y, cw2, greetH, 20, icGreetBg)
	cv.fillRoundRect(x0, y, cw2, greetH, 20, icGreetBorder)
	cv.text(x0+32, y+58, "老板好，", 26, false, icInk2)
	cv.text(x0+32, y+106, "以下是 "+reportBrand+" 昨天的使用情况汇总，请查阅。", 26, false, icInk2)
	cv.borderedPill(x0+32, y+134, 46, "考核标准：每日登录 ≥ 1 次 且 对话 ≥ 2 条 为达标", icRuleBg, icRuleBorder, icBlue)
	y += greetH + gapModule

	// --- 四联指标瓦片（1.2 / 1 / 1 / 1.2 比例；数字保留字重） ---
	gap := 18
	unit := int(float64(cw2-gap*3) / 4.4)
	w1, w2 := int(float64(unit)*1.2), unit
	w4 := cw2 - w1 - w2*2 - gap*3
	cv.statTile(x0, y, w1, icBlue, "整体达标率", rate, fmt.Sprintf("达标 %d 人 / 总 %d 人", r.Qualified, r.TotalUsers))
	cv.statTile(x0+w1+gap, y, w2, icInk, "总用户数", fmt.Sprintf("%d", r.TotalUsers), fmt.Sprintf("活跃用户 %d 人", activeUsers))
	cv.statTile(x0+w1+gap+w2+gap, y, w2, icGreen, "达标用户", fmt.Sprintf("%d", r.Qualified), "达标率 "+pct(r.Qualified, r.TotalUsers))
	cv.statTile(x0+w1+gap*3+w2*2, y, w4, icRed, "未达标用户", fmt.Sprintf("%d", r.Unqualified), "未达标率 "+pct(r.Unqualified, r.TotalUsers))
	y += tileH + gapSmall

	// --- 昨日消息胶囊（数字加粗，其余常规） ---
	{
		lbl := "昨日消息"
		lw := cv.textWidth(lbl, 26, false)
		num := fmt.Sprintf("%d", r.TotalMessages)
		nw := cv.textWidth(num, 38, true)
		msgDelta := deltaPercent(r.TotalMessages, r.PrevMessages)
		chipCol := icBlue
		if msgDelta == "持平" {
			chipCol = icMuted
		} else if strings.HasPrefix(msgDelta, "-") {
			chipCol = icRedBadge
		}
		tw2 := cv.textWidth(msgDelta, 23, false)
		pw := lw + nw + tw2 + 122
		ph := 68
		cv.fillRoundRect(x0, y, pw, ph, ph/2, icMsgBg)
		cv.text(x0+30, y+ph/2+9, lbl, 26, false, reportColor{0x2C, 0x3E, 0x5A})
		cv.text(x0+30+lw+18, y+ph/2+13, num, 38, true, icInk)
		cxp := x0 + 30 + lw + 18 + nw + 16
		cv.fillRoundRect(cxp, y+16, tw2+32, ph-32, (ph-32)/2, icBlueChip)
		cv.text(cxp+16, y+ph/2+8, msgDelta, 23, false, chipCol)
	}
	y += 68 + gapSmall

	// --- 明细表：圆角描边容器，行高放大（清单三.3.2） ---
	tx := x0
	tw := cw2
	cv.fillRoundRect(tx, y, tw, tableH, 18, icTileBorder)
	cv.fillRoundRect(tx+1, y+1, tw-2, tableH-2, 17, icCard)
	cv.fillRoundRect(tx+1, y+1, tw-2, headRowH, 17, icThBg)
	colLogin := tx + 400
	colChat := tx + 520
	colStatus := tx + 640
	activeRight := tx + tw - 26
	hh := y + headRowH/2 + 9
	cv.text(tx+26, hh, "用户", 24, true, icThText)
	cv.textCenter(colLogin, hh, "登录", 24, true, icThText)
	cv.textCenter(colChat, hh, "对话", 24, true, icThText)
	cv.textCenter(colStatus, hh, "状态", 24, true, icThText)
	cv.textRight(activeRight, hh, "上次活跃", 24, true, icThText)
	cv.fillRect(tx+1, y+headRowH, tw-2, 1, reportColor{0xE6, 0xED, 0xF5})
	rowY := y + headRowH
	for i, row := range rows {
		base := rowY + imgRowH/2 + 10
		cv.text(tx+26, base, cv.truncateMiddle(reportDisplayName(row.Username), 336, 27, false), 27, false, icInk)
		cv.textCenter(colLogin, base, fmt.Sprintf("%d", row.Logins), 26, false, icInk2)
		cv.textCenter(colChat, base, fmt.Sprintf("%d", row.Chats), 26, false, icInk2)
		if row.Qualified {
			cv.pill(colStatus, rowY+imgRowH/2, "达标", icGreenBg, icGreen, 22)
		} else {
			cv.pill(colStatus, rowY+imgRowH/2, "未达标", icRedBg, icRedBadge, 22)
		}
		cv.textRight(activeRight, base, lastActiveText(row.LastActive, now), 22, false, icLastActive)
		if i < len(rows)-1 || hidden > 0 {
			cv.fillRect(tx+26, rowY+imgRowH, tw-52, 1, icRowSep)
		}
		rowY += imgRowH
	}
	if hidden > 0 {
		cv.text(tx+26, rowY+34, fmt.Sprintf("其余 %d 人略", hidden), 22, false, icFooterGray)
		rowY += 48
	}
	y += tableH + gapModule

	// --- 页脚：徽章 + 生成时间 + 落款（正文常规字重，数字加粗） ---
	cv.fillRect(x0, y, cw2, 1, icSep)
	fy := y + 26
	{
		lbl := "整体达标率"
		lw := cv.textWidth(lbl, 25, false)
		rw := cv.textWidth(rate, 30, true)
		pw := lw + rw + 64
		cv.fillRoundRect(x0, fy, pw, 54, 27, icTrendBg)
		cv.text(x0+28, fy+35, lbl, 25, false, icBlue)
		cv.text(x0+28+lw+12, fy+37, rate, 30, true, icBlue)
		trend := "较前日持平"
		if diffPP > 0 {
			trend = fmt.Sprintf("较前日 +%.1f 个百分点，趋势向好", diffPP)
		} else if diffPP < 0 {
			trend = fmt.Sprintf("较前日 %.1f 个百分点，有所回落", diffPP)
		}
		tw2 := cv.textWidth(trend, 25, false)
		cxp := x0 + pw + 16
		cv.fillRoundRect(cxp, fy, tw2+60, 54, 27, icCompareBg)
		cv.text(cxp+30, fy+35, trend, 25, false, reportColor{0x1D, 0x3A, 0x5A})
		gen := "报告生成于 " + now.Format("2006-01-02 15:04")
		cv.textRight(x0+cw2, fy+35, gen, 22, false, icMuted)
	}
	// 落款两行，与上方拉开距离（清单五.4）
	extra1 := "本报告由 " + reportBrand + " 自动生成 · 如有疑问请联系管理员"
	extra2 := fmt.Sprintf("© %d %s · 使用汇总日报", now.Year(), reportBrand)
	e1w := cv.textWidth(extra1, 21, false)
	e2w := cv.textWidth(extra2, 21, false)
	cv.text(x0+(cw2-e1w)/2, fy+112, extra1, 21, false, icFooterGray)
	cv.text(x0+(cw2-e2w)/2, fy+148, extra2, 21, false, icFooterGray)

	finalH := fy + 168 + pageMargin
	if finalH > totalH {
		finalH = totalH
	}
	cropped := cv.img.SubImage(image.Rect(0, 0, imgW, finalH)).(*image.RGBA)

	var buf bytes.Buffer
	if err := png.Encode(&buf, cropped); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// --- 企微图片上传与发送 ---

func (s *usageReportService) uploadWeComImage(ctx context.Context, accessToken string, pngBytes []byte) (string, error) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("media", "usage-report.png")
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(pngBytes); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	uploadURL := "https://qyapi.weixin.qq.com/cgi-bin/media/upload?access_token=" + url.QueryEscape(accessToken) + "&type=image"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MediaID string `json:"media_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 || result.MediaID == "" {
		return "", fmt.Errorf("wecom media/upload failed: %s (errcode=%d)", result.ErrMsg, result.ErrCode)
	}
	return result.MediaID, nil
}

func (s *usageReportService) sendWeComImage(ctx context.Context, tenant *types.Tenant, recipients []string, mediaID string) error {
	w := tenant.SSOConfig.WeCom
	agentID := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(w.AgentID), "%d", &agentID); err != nil {
		return fmt.Errorf("invalid WeCom agent_id %q: %w", w.AgentID, err)
	}
	token, err := wecomReportAccessToken(ctx, w.CorpID, w.CorpSecret)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"touser":  strings.Join(recipients, "|"),
		"msgtype": "image",
		"agentid": agentID,
		"image":   map[string]string{"media_id": mediaID},
	}
	body, _ := json.Marshal(payload)
	sendURL := "https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result wecomSendResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom message/send(image) failed: %s (errcode=%d)", result.ErrMsg, result.ErrCode)
	}
	result.logPartialFailures(ctx, recipients)
	return nil
}

// pushWeComImage 上传图片并发送（media_id 三天有效，即时发送无虞）。
func (s *usageReportService) pushWeComImage(ctx context.Context, tenant *types.Tenant, recipients []string, pngBytes []byte) error {
	w := tenant.SSOConfig.WeCom
	if w == nil {
		return fmt.Errorf("workspace has no WeCom app credentials")
	}
	token, err := wecomReportAccessToken(ctx, w.CorpID, w.CorpSecret)
	if err != nil {
		return err
	}
	mediaID, err := s.uploadWeComImage(ctx, token, pngBytes)
	if err != nil {
		return err
	}
	return s.sendWeComImage(ctx, tenant, recipients, mediaID)
}

// RenderUsageReportImageForHandler 导出给 handler 的渲染入口（测试预览用，
// 与推送图片同一渲染管线）。
func RenderUsageReportImageForHandler(r *UsageReport, now time.Time) ([]byte, error) {
	return renderUsageReportImage(r, now)
}

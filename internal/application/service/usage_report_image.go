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
	"golang.org/x/image/vector"

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
	imgW = 1320
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

// fillRoundRect 圆角矩形（抗锯齿）：vector 光栅化器平滑光栅化后
// DrawMask 合成，边缘与浏览器 CSS 圆角观感一致（扫描线版是阶梯锯齿）。
func (c *reportCanvas) fillRoundRect(x, y, w, h, r int, col reportColor) {
	if w <= 0 || h <= 0 {
		return
	}
	rf := float32(r)
	if rf > float32(w)/2 {
		rf = float32(w) / 2
	}
	if rf > float32(h)/2 {
		rf = float32(h) / 2
	}
	xF, yF, wF, hF := float32(x), float32(y), float32(w), float32(h)
	k := rf * 0.55228475 // 圆弧的四分之一 cubic bezier 常数
	rast := vector.NewRasterizer(w, h)
	rast.MoveTo(xF+rf, yF)
	rast.LineTo(xF+wF-rf, yF)
	rast.CubeTo(xF+wF-rf+k, yF, xF+wF, yF+rf-k, xF+wF, yF+rf)
	rast.LineTo(xF+wF, yF+hF-rf)
	rast.CubeTo(xF+wF, yF+hF-rf+k, xF+wF-rf+k, yF+hF, xF+wF-rf, yF+hF)
	rast.LineTo(xF+rf, yF+hF)
	rast.CubeTo(xF+rf-k, yF+hF, xF, yF+hF-rf+k, xF, yF+hF-rf)
	rast.LineTo(xF, yF+rf)
	rast.CubeTo(xF, yF+rf-k, xF+rf-k, yF, xF+rf, yF)
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	rast.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	draw.DrawMask(c.img, image.Rect(x, y, x+w, y+h),
		&image.Uniform{col.rgba()}, image.Point{}, mask, image.Point{}, draw.Over)
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

// statTile 居中指标瓦片（HTML 模板实测 ×1.52）。
func (c *reportCanvas) statTile(x, y, w int, valueCol reportColor, label, value, sub string) {
	// 模板 .metric-item：完整 1px 圆角描边（border: 1px solid #eef3fa）
	c.fillRoundRect(x, y, w, 180, 22, icTileBorder)
	c.fillRoundRect(x+1, y+1, w-2, 178, 21, icTileBg)
	lw := c.textWidth(label, 20, false)
	c.text(x+(w-lw)/2, y+52, label, 20, false, icLabel)
	vw := c.textWidth(value, 49, true)
	c.text(x+(w-vw)/2, y+118, value, 49, true, valueCol)
	sw := c.textWidth(sub, 20, false)
	c.text(x+(w-sw)/2, y+156, sub, 20, false, icLabel)
}

// renderUsageReportImage 绘制整份日报为 PNG。几何参数 = 模板 HTML 在
// 868px 视口下的实测值 ×1.52（=1320/868）；文案固定品牌名「智能知识库」；
// 正文常规字重，仅标题/表头/指标数字/徽章加粗；页脚生成时间与趋势
// 胶囊同行右侧。模板 emoji 在 CJK 矢量字体下渲染为方框，省略。
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

	tableH := 72 + len(rows)*76 + 14
	if hidden > 0 {
		tableH += 54
	}
	// 顺序排版 + 220px 余量，画完裁剪。
	totalH := pageMargin + 110 + 42 + 240 + 48 + 180 + 42 + 54 + 30 +
		tableH + 42 + 220 + 48 + pageMargin + 220

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

	// ===== 干跑测量：先确定问候区布局与页脚换行，得到卡片精确高度 =====
	x0 := pageMargin + cardPad
	cw2 := imgW - pageMargin*2 - cardPad*2
	measureRule := "考核标准：每日登录 ≥ 1 次 且 对话 ≥ 2 条 为达标"
	measureL2 := "以下是 " + reportBrand + " 昨天的使用情况汇总，请查阅。"
	measureL1 := "老板好，"
	ruleW := cv.textWidth(measureRule, 21, false) + 52
	greetSideBySide := 36+cv.textWidth(measureL1, 23, false)+ruleW+36 <= cw2 &&
		36+cv.textWidth(measureL2, 23, false)+ruleW+36 <= cw2
	greetH := 210
	if !greetSideBySide {
		greetH = 240
	}
	fOff := 0
	{
		lw := cv.textWidth("整体达标率", 21, false)
		rw2 := cv.textWidth(rate, 24, true)
		pw := lw + 8 + rw2 + 52
		trend := "较前日持平"
		if diffPP > 0 {
			trend = fmt.Sprintf("较前日 +%.1f 个百分点，趋势向好", diffPP)
		} else if diffPP < 0 {
			trend = fmt.Sprintf("较前日 %.1f 个百分点，有所回落", diffPP)
		}
		tw2 := cv.textWidth(trend, 21, false)
		genW := cv.textWidth("报告生成于 "+now.Format("2006-01-02 15:04"), 20, false)
		if pw+24+tw2+48+28+genW > cw2 {
			fOff = 66
		}
	}
	// 各模块高度：头部 110+42 / 问候 greetH+48 / 瓦片 180+42 / 消息 54+30 /
	// 表格 tableH+42 / 页脚 30+192+6+44（含底部内边距 44≈28×1.52）。
	cardH := 152 + greetH + 48 + 222 + 84 + tableH + 42 + 272 + fOff
	cardW := imgW - pageMargin*2

	// ===== 白色大卡片（模板 .card 白底） =====
	cv.fillRoundRect(pageMargin, pageMargin, cardW, cardH, cardRadius, icCard)
	y := pageMargin

	// --- 头部（padb 24 / mb 42） ---
	cv.text(x0, y+46, reportBrand+" · 使用日报", 33, true, icInk)
	cv.text(x0, y+82, fmt.Sprintf("%s（%s）", r.Date, weekday), 21, false, icMuted)
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
		lw := cv.textWidth(lbl, 21, false)
		cw2v := cv.textWidth(deltaTxt, 19, true)
		pw := lw + cw2v + 22 + 14 + 26 + 24
		ph := 54
		px := x0 + cw2 - pw
		py := y + 110 - 24 - ph
		cv.fillRoundRect(px, py, pw, ph, ph/2, icBlueBg)
		cv.text(px+24, py+ph/2+7, lbl, 21, false, icBlue)
		cv.fillRoundRect(px+24+lw+14, py+11, cw2v+30, ph-22, (ph-22)/2, chipBg)
		cv.text(px+24+lw+14+15, py+ph/2+7, deltaTxt, 19, true, chipFg)
	}
	cv.fillRect(x0, y+110, cw2, 1, icSep)
	y += 110 + 42

	// --- 问候区（文本 / 考核胶囊；放得下并排，否则堆叠） ---
	ruleTxt := measureRule
	line1 := measureL1
	line2 := measureL2
	rw := ruleW
	sideBySide := greetSideBySide
	cv.fillRoundRect(x0, y, cw2, greetH, 30, icGreetBg)
	cv.fillRoundRect(x0, y, cw2, greetH, 30, icGreetBorder)
	drawRulePill := func(px, py int) {
		pw := rw
		ph := 50
		cv.fillRoundRect(px, py, pw, ph, ph/2, icRuleBorder)
		cv.fillRoundRect(px+1, py+1, pw-2, ph-2, ph/2-1, icRuleBg)
		cv.text(px+26, py+ph/2+7, ruleTxt, 21, false, icBlue)
	}
	if sideBySide {
		cv.text(x0+36, y+82, line1, 23, false, icInk2)
		cv.text(x0+36, y+124, line2, 23, false, icInk2)
		drawRulePill(x0+cw2-36-rw, y+(greetH-50)/2)
	} else {
		cv.text(x0+36, y+70, line1, 23, false, icInk2)
		cv.text(x0+36, y+128, line2, 23, false, icInk2)
		drawRulePill(x0+36, y+162)
	}
	y += greetH + 48

	// --- 四联指标瓦片（1.2 / 1 / 1 / 1.2） ---
	gap := 21
	unit := int(float64(cw2-gap*3) / 4.4)
	w1 := int(float64(unit) * 1.2)
	w2 := unit
	w4 := cw2 - w1 - w2*2 - gap*3
	cv.statTile(x0, y, w1, icBlue, "整体达标率", rate, fmt.Sprintf("达标 %d 人 / 总 %d 人", r.Qualified, r.TotalUsers))
	cv.statTile(x0+w1+gap, y, w2, icInk, "总用户数", fmt.Sprintf("%d", r.TotalUsers), fmt.Sprintf("活跃用户 %d 人", activeUsers))
	cv.statTile(x0+w1+gap+w2+gap, y, w2, icGreen, "达标用户", fmt.Sprintf("%d", r.Qualified), "达标率 "+pct(r.Qualified, r.TotalUsers))
	cv.statTile(x0+w1+gap*3+w2*2, y, w4, icRed, "未达标用户", fmt.Sprintf("%d", r.Unqualified), "未达标率 "+pct(r.Unqualified, r.TotalUsers))
	y += 180 + 42

	// --- 昨日消息胶囊（h54） ---
	{
		lbl := "昨日消息"
		lw := cv.textWidth(lbl, 21, false)
		num := fmt.Sprintf("%d", r.TotalMessages)
		nw := cv.textWidth(num, 30, true)
		msgDelta := deltaPercent(r.TotalMessages, r.PrevMessages)
		chipCol := icBlue
		if msgDelta == "持平" {
			chipCol = icMuted
		} else if strings.HasPrefix(msgDelta, "-") {
			chipCol = icRedBadge
		}
		tw2 := cv.textWidth(msgDelta, 20, false)
		pw := 30 + lw + 14 + nw + 14 + tw2 + 36 + 28
		ph := 54
		cv.fillRoundRect(x0, y, pw, ph, ph/2, icMsgBg)
		cv.text(x0+30, y+ph/2+7, lbl, 21, false, reportColor{0x2C, 0x3E, 0x5A})
		cv.text(x0+30+lw+14, y+ph/2+10, num, 30, true, icInk)
		cxp := x0 + 30 + lw + 14 + nw + 14
		cv.fillRoundRect(cxp, y+16, tw2+36, ph-32, (ph-32)/2, icBlueChip)
		cv.text(cxp+18, y+ph/2+6, msgDelta, 20, false, chipCol)
	}
	y += 54 + 30

	// --- 明细表（圆角描边容器，行高 76） ---
	tx := x0
	tw := cw2
	cv.fillRoundRect(tx, y, tw, tableH, 22, icTileBorder)
	cv.fillRoundRect(tx+1, y+1, tw-2, tableH-2, 21, icCard)
	cv.fillRoundRect(tx+1, y+1, tw-2, 72, 21, icThBg)
	colLogin := tx + 500
	colChat := tx + 670
	colStatus := tx + 840
	activeRight := tx + tw - 24
	hh := y + 45
	cv.text(tx+24, hh, "用户", 20, true, icThText)
	cv.textCenter(colLogin, hh, "登录", 20, true, icThText)
	cv.textCenter(colChat, hh, "对话", 20, true, icThText)
	cv.textCenter(colStatus, hh, "状态", 20, true, icThText)
	cv.textRight(activeRight, hh, "上次活跃", 20, true, icThText)
	cv.fillRect(tx+1, y+72, tw-2, 1, reportColor{0xE6, 0xED, 0xF5})
	rowY := y + 72
	for i, row := range rows {
		base := rowY + 45
		cv.text(tx+24, base, cv.truncateMiddle(reportDisplayName(row.Username), 420, 21, false), 21, false, icInk)
		cv.textCenter(colLogin, base, fmt.Sprintf("%d", row.Logins), 21, false, icInk2)
		cv.textCenter(colChat, base, fmt.Sprintf("%d", row.Chats), 21, false, icInk2)
		if row.Qualified {
			cv.pill(colStatus, rowY+38, "达标", icGreenBg, icGreen, 18)
		} else {
			cv.pill(colStatus, rowY+38, "未达标", icRedBg, icRedBadge, 18)
		}
		cv.textRight(activeRight, base, lastActiveText(row.LastActive, now), 20, false, icLastActive)
		if i < len(rows)-1 || hidden > 0 {
			cv.fillRect(tx+24, rowY+76, tw-48, 1, icRowSep)
		}
		rowY += 76
	}
	if hidden > 0 {
		cv.text(tx+24, rowY+36, fmt.Sprintf("其余 %d 人略", hidden), 20, false, icFooterGray)
		rowY += 54
	}
	y += tableH + 42

	// --- 页脚：胶囊行（右侧同排生成时间）+ 居中落款 ---
	cv.fillRect(x0, y, cw2, 1, icSep)
	fy := y + 30
	extraOff := fOff
	{
		lbl := "整体达标率"
		lw := cv.textWidth(lbl, 21, false)
		rw2 := cv.textWidth(rate, 24, true)
		pw := lw + 8 + rw2 + 52
		cv.fillRoundRect(x0, fy, pw, 52, 26, icTrendBg)
		cv.text(x0+26, fy+33, lbl, 21, false, icBlue)
		cv.text(x0+26+lw+8, fy+35, rate, 24, true, icBlue)
		trend := "较前日持平"
		if diffPP > 0 {
			trend = fmt.Sprintf("较前日 +%.1f 个百分点，趋势向好", diffPP)
		} else if diffPP < 0 {
			trend = fmt.Sprintf("较前日 %.1f 个百分点，有所回落", diffPP)
		}
		tw2 := cv.textWidth(trend, 21, false)
		cxp := x0 + pw + 24
		cv.fillRoundRect(cxp, fy, tw2+48, 52, 26, icCompareBg)
		cv.text(cxp+24, fy+33, trend, 21, false, reportColor{0x1D, 0x3A, 0x5A})
		// 生成时间与胶囊同行右对齐、垂直居中（1320 宽度下充裕；极端
		// 宽字体放不下才退到下一行，落款同步下移）。
		gen := "报告生成于 " + now.Format("2006-01-02 15:04")
		genBase := fy + 33
		if extraOff > 0 {
			genBase = fy + 52 + 14 + 26
		}
		cv.textRight(x0+cw2, genBase, gen, 20, false, icMuted)
	}
	cv.fillRect(x0, fy+96+extraOff, cw2, 1, reportColor{0xF0, 0xF4, 0xFA})
	extra1 := "本报告由 " + reportBrand + " 自动生成 · 如有疑问请联系管理员"
	extra2 := fmt.Sprintf("© %d %s · 使用汇总日报", now.Year(), reportBrand)
	e1w := cv.textWidth(extra1, 18, false)
	e2w := cv.textWidth(extra2, 18, false)
	cv.text(x0+(cw2-e1w)/2, fy+150+extraOff, extra1, 18, false, icFooterGray)
	cv.text(x0+(cw2-e2w)/2, fy+192+extraOff, extra2, 18, false, icFooterGray)

	finalH := fy + 216 + extraOff + pageMargin
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

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

	"github.com/Tencent/WeKnora/internal/types"
)

// 报告图片渲染：企微 markdown 消息在部分客户端（微信插件等）不渲染，
// 纯文本缺乏层次；把日报绘制成 PNG 通过 media/upload + image 消息发送，
// 所有客户端显示一致。字体取自运行镜像的 fonts-noto-cjk；缺失或渲染
// 失败时回退纯文本消息。

const (
	reportImgWidth      = 1000
	reportImgPad        = 48
	reportImgCardH      = 132
	reportImgCardGap    = 20
	reportImgRowH       = 52
	reportImgTableHeadH = 58
	reportImgMaxImgRows = 30
)

type reportColor struct {
	r, g, b uint8
}

func (c reportColor) rgba() color.RGBA { return color.RGBA{c.r, c.g, c.b, 255} }

var (
	rcPrimary     = reportColor{0x1F, 0x29, 0x37}
	rcSecondary   = reportColor{0x6B, 0x72, 0x80}
	rcPlaceholder = reportColor{0x9C, 0xA3, 0xAF}
	rcGreen       = reportColor{0x05, 0x96, 0x69}
	rcGreenBg     = reportColor{0xEC, 0xFD, 0xF5}
	rcOrange      = reportColor{0xD9, 0x77, 0x06}
	rcOrangeBg    = reportColor{0xFF, 0xFB, 0xEB}
	rcBlue        = reportColor{0x25, 0x63, 0xEB}
	rcBlueBg      = reportColor{0xEF, 0xF6, 0xFF}
	rcGrayBg      = reportColor{0xF9, 0xFA, 0xFB}
	rcBorder      = reportColor{0xE5, 0xE7, 0xEB}
)

// --- 字体（进程内一次性加载，面按需创建） ---

var (
	reportFontsOnce sync.Once
	reportFontReg   *opentype.Font
	reportFontBold  *opentype.Font
	reportFontsErr  error
)

func findNotoCJK(preferBold bool) string {
	ordered := []string{"NotoSansCJK-Regular.ttc", "NotoSansCJK-Bold.ttc"}
	if preferBold {
		ordered = []string{"NotoSansCJK-Bold.ttc", "NotoSansCJK-Regular.ttc"}
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
	img    *image.RGBA
	reg    *opentype.Font
	bold   *opentype.Font
	faces  map[string]font.Face
	nextY  int
	sizes  map[float64]fixed.Int26_6
	metric map[string]fixed.Int26_6
}

func newReportCanvas(height int) (*reportCanvas, error) {
	reportFontsOnce.Do(func() {
		reportFontReg, reportFontBold, reportFontsErr = loadReportFonts()
	})
	if reportFontsErr != nil {
		return nil, reportFontsErr
	}
	return &reportCanvas{
		img:    image.NewRGBA(image.Rect(0, 0, reportImgWidth, height)),
		reg:    reportFontReg,
		bold:   reportFontBold,
		faces:  map[string]font.Face{},
		sizes:  map[float64]fixed.Int26_6{},
		metric: map[string]fixed.Int26_6{},
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

func (c *reportCanvas) lineHeight(size float64, bold bool) int {
	key := fmt.Sprintf("lh-%v-%v", size, bold)
	if v, ok := c.metric[key]; ok {
		return v.Ceil()
	}
	f, err := c.face(size, bold)
	if err != nil {
		return int(size * 1.6)
	}
	v := f.Metrics().Height
	c.metric[key] = v
	return v.Ceil()
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

func (c *reportCanvas) fillRect(x, y, w, h int, col reportColor) {
	draw.Draw(c.img, image.Rect(x, y, x+w, y+h), &image.Uniform{col.rgba()}, image.Point{}, draw.Src)
}

func (c *reportCanvas) hline(x1, x2, y int, col reportColor) {
	c.fillRect(x1, y, x2-x1, 1, col)
}

func (c *reportCanvas) vline(x, y1, y2 int, col reportColor) {
	c.fillRect(x, y1, 1, y2-y1, col)
}

// statCard 画一个统计卡：左侧色条 + 标签 + 大数字 + 副文本。
func (c *reportCanvas) statCard(x, y, w int, accent, bg reportColor, label, value, sub string) {
	c.fillRect(x, y, w, reportImgCardH, bg)
	c.fillRect(x, y, 6, reportImgCardH, accent)
	c.fillRect(x, y, w, 1, rcBorder)
	c.fillRect(x, y+reportImgCardH-1, w, 1, rcBorder)
	c.fillRect(x+w-1, y, 1, reportImgCardH, rcBorder)

	lh := c.lineHeight(24, false)
	c.text(x+24, y+30+lh/2, label, 24, false, rcSecondary)
	vy := y + 34 + lh + 44
	c.text(x+24, vy, value, 46, true, accent)
	if sub != "" {
		c.textRight(x+w-24, vy, sub, 24, false, rcPlaceholder)
	}
}

// renderUsageReportImage 绘制整份日报为 PNG。
func renderUsageReportImage(r *UsageReport, now time.Time) ([]byte, error) {
	weekday := map[string]string{"Sunday": "周日", "Monday": "周一", "Tuesday": "周二", "Wednesday": "周三", "Thursday": "周四", "Friday": "周五", "Saturday": "周六"}[now.Format("Monday")]

	rows := r.Rows
	hidden := 0
	if len(rows) > reportImgMaxImgRows {
		hidden = len(rows) - reportImgMaxImgRows
		rows = rows[:reportImgMaxImgRows]
	}

	// 预估高度（渲染前需要一个画布尺寸；字体行高按字号的 1.55 估，
	// 额外 +160px 余量，画完后按实际内容裁剪）。
	headH := reportImgPad + 56 + 44
	cardsH := (reportImgCardH*2 + reportImgCardGap) + 28
	ruleH := 48
	tableH := reportImgTableHeadH + len(rows)*reportImgRowH + 12
	if hidden > 0 {
		tableH += 40
	}
	summaryH := 120 + 20
	footH := 48 + reportImgPad
	totalH := headH + cardsH + ruleH + tableH + summaryH + footH + 160

	cv, err := newReportCanvas(totalH)
	if err != nil {
		return nil, err
	}
	defer cv.close()
	draw.Draw(cv.img, cv.img.Bounds(), &image.Uniform{color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}}, image.Point{}, draw.Src)

	// 标题与日期
	cv.text(reportImgPad, reportImgPad+50, "📊 "+r.TenantName+" 使用日报", 44, true, rcPrimary)
	cv.text(reportImgPad, reportImgPad+50+52, fmt.Sprintf("%s（%s）", r.Date, weekday), 26, false, rcSecondary)

	y := reportImgPad + 56 + 44 + 24

	// 2×2 统计卡
	cardW := (reportImgWidth - reportImgPad*2 - reportImgCardGap) / 2
	cv.statCard(reportImgPad, y, cardW, rcBlue, rcBlueBg, "总用户数", fmt.Sprintf("%d", r.TotalUsers), "")
	cv.statCard(reportImgPad+cardW+reportImgCardGap, y, cardW, rcGreen, rcGreenBg, "达标用户", fmt.Sprintf("%d", r.Qualified), pct(r.Qualified, r.TotalUsers))
	y += reportImgCardH + reportImgCardGap
	cv.statCard(reportImgPad, y, cardW, rcOrange, rcOrangeBg, "未达标用户", fmt.Sprintf("%d", r.Unqualified), pct(r.Unqualified, r.TotalUsers))
	cv.statCard(reportImgPad+cardW+reportImgCardGap, y, cardW, rcPrimary, rcGrayBg, "昨日消息总数", fmt.Sprintf("%d", r.TotalMessages), "较前日 "+deltaPercent(r.TotalMessages, r.PrevMessages))
	y += reportImgCardH + 28

	// 达标标准
	cv.text(reportImgPad, y+28, "达标标准：登录 ≥1 次 且 对话 ≥2 次", 26, false, rcSecondary)
	y += ruleH

	// 明细表
	tableW := reportImgWidth - reportImgPad*2
	x0, x1 := reportImgPad, reportImgPad+tableW
	colLogin := x0 + 360
	colChat := x0 + 480
	colStatus := x0 + 600
	cv.fillRect(x0, y, tableW, reportImgTableHeadH, rcGrayBg)
	hh := reportImgTableHeadH/2 + 14
	cv.text(x0+20, y+hh, "用户", 26, true, rcSecondary)
	cv.textCenter((colLogin+colChat)/2, y+hh, "登录", 26, true, rcSecondary)
	cv.textCenter((colChat+colStatus)/2, y+hh, "对话", 26, true, rcSecondary)
	cv.textCenter((colStatus+x1)/2, y+hh, "状态", 26, true, rcSecondary)
	cv.text(x1-260, y+hh, "上次活跃", 26, true, rcSecondary)
	cv.hline(x0, x1, y+reportImgTableHeadH, rcBorder)
	rowY := y + reportImgTableHeadH
	for i, row := range rows {
		if i%2 == 1 {
			cv.fillRect(x0, rowY, tableW, reportImgRowH, rcGrayBg)
		}
		base := rowY + reportImgRowH/2 + 10
		cv.text(x0+20, base, cv.truncate(row.Username, 320, 28, false), 28, false, rcPrimary)
		cv.textCenter((colLogin+colChat)/2, base, fmt.Sprintf("%d", row.Logins), 28, false, rcPrimary)
		cv.textCenter((colChat+colStatus)/2, base, fmt.Sprintf("%d", row.Chats), 28, false, rcPrimary)
		if row.Qualified {
			cv.textCenter((colStatus+x1)/2, base, "达标", 28, true, rcGreen)
		} else {
			cv.textCenter((colStatus+x1)/2, base, "未达标", 28, true, rcOrange)
		}
		cv.text(x1-260, base, lastActiveText(row.LastActive, now), 26, false, rcSecondary)
		cv.hline(x0, x1, rowY+reportImgRowH, rcBorder)
		rowY += reportImgRowH
	}
	if hidden > 0 {
		cv.text(x0+20, rowY+34, fmt.Sprintf("…其余 %d 人略", hidden), 26, false, rcPlaceholder)
		rowY += 44
	}
	y = rowY + 12

	// 昨日小结（浅色块）
	cur := ratePermille(r.Qualified, r.TotalUsers)
	prev := ratePermille(r.PrevQualified, r.TotalUsers)
	diffPP := float64(cur-prev) / 10.0
	trend := "➖ 与前日持平"
	trendCol := rcSecondary
	switch {
	case cur > prev:
		trend = fmt.Sprintf("📈 趋势向好（较前日 +%.1f 个百分点）", diffPP)
		trendCol = rcGreen
	case cur < prev:
		trend = fmt.Sprintf("📉 有所回落（较前日 %.1f 个百分点）", diffPP)
		trendCol = rcOrange
	}
	cv.fillRect(x0, y, tableW, 104, rcBlueBg)
	cv.text(x0+24, y+44, fmt.Sprintf("整体达标率 %s", pct(r.Qualified, r.TotalUsers)), 32, true, rcPrimary)
	cv.text(x0+24, y+84, trend, 26, false, trendCol)
	y += 104 + 20

	// 页脚
	cv.text(reportImgPad, y+28, "报告生成："+now.Format("2006-01-02 15:04"), 24, false, rcPlaceholder)
	finalH := y + 28 + 24
	if finalH > totalH {
		finalH = totalH
	}
	cropped := cv.img.SubImage(image.Rect(0, 0, reportImgWidth, finalH)).(*image.RGBA)

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
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom message/send(image) failed: %s (errcode=%d)", result.ErrMsg, result.ErrCode)
	}
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

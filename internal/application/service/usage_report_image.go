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
	imgW          = 1080
	imgPad        = 48
	cardRadius    = 20
	imgTileH      = 170
	imgRowH       = 56
	imgTableHeadH = 56
	imgMaxRows    = 30
)

type reportColor struct{ r, g, b uint8 }

func (c reportColor) rgba() color.RGBA { return color.RGBA{c.r, c.g, c.b, 255} }

var (
	icCanvas    = reportColor{0xF6, 0xF7, 0xF9} // 画布底
	icCard      = reportColor{0xFF, 0xFF, 0xFF} // 卡片底
	icBanner    = reportColor{0x1E, 0x29, 0x3B} // 头部深色横幅
	icOnDark    = reportColor{0xFF, 0xFF, 0xFF}
	icOnDarkSub = reportColor{0x94, 0xA3, 0xB8}
	icPrimary   = reportColor{0x0F, 0x17, 0x2A}
	icSecondary = reportColor{0x64, 0x74, 0x8B}
	icMuted     = reportColor{0x94, 0xA3, 0xB8}
	icDivider   = reportColor{0xF1, 0xF5, 0xF9}
	icHeadBg    = reportColor{0xF1, 0xF5, 0xF9}
	icTrack     = reportColor{0xE2, 0xE8, 0xF0}
	icBlue      = reportColor{0x25, 0x63, 0xEB}
	icBlueBg    = reportColor{0xEF, 0xF6, 0xFF}
	icGreen     = reportColor{0x05, 0x96, 0x69}
	icGreenBg   = reportColor{0xEC, 0xFD, 0xF5}
	icOrange    = reportColor{0xD9, 0x77, 0x06}
	icOrangeBg  = reportColor{0xFF, 0xF7, 0xE8}
	icSlate     = reportColor{0x33, 0x41, 0x55}
	icSlateBg   = reportColor{0xF1, 0xF5, 0xF9}
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

// pill 胶囊徽章：低饱和底 + 语义色文字。
func (c *reportCanvas) pill(cx, cy int, label string, bg, fg reportColor) {
	tw := c.textWidth(label, 24, true)
	pw := tw + 44
	ph := 40
	c.fillRoundRect(cx-pw/2, cy-ph/2, pw, ph, ph/2, bg)
	c.textCenter(cx, cy+9, label, 24, true, fg)
}

// statTile 统计瓦片：圆角浅底、顶部标签、大数字、下方副文本，统一左对齐。
func (c *reportCanvas) statTile(x, y, w int, accent, bg reportColor, label, value, sub string, subCol reportColor) {
	c.fillRoundRect(x, y, w, imgTileH, cardRadius, bg)
	c.text(x+26, y+52, label, 24, false, icSecondary)
	c.text(x+26, y+118, value, 50, true, accent)
	if sub != "" {
		c.text(x+26, y+152, sub, 22, false, subCol)
	}
}

// reportDisplayName 明细表里去掉 wecom_ 前缀，长账号更可读。
func reportDisplayName(u string) string {
	return strings.TrimPrefix(u, "wecom_")
}

var reportWeekdays = map[string]string{
	"Sunday": "周日", "Monday": "周一", "Tuesday": "周二", "Wednesday": "周三",
	"Thursday": "周四", "Friday": "周五", "Saturday": "周六",
}

// renderUsageReportImage 绘制整份日报为 PNG。
func renderUsageReportImage(r *UsageReport, now time.Time) ([]byte, error) {
	rows := r.Rows
	hidden := 0
	if len(rows) > imgMaxRows {
		hidden = len(rows) - imgMaxRows
		rows = rows[:imgMaxRows]
	}

	bannerH := 150
	tileGap := 16
	captionH := 46
	tableCardH := 24 + imgTableHeadH + len(rows)*imgRowH + 20
	if hidden > 0 {
		tableCardH += 46
	}
	summaryH := 164
	footH := 60
	totalH := imgPad + bannerH + 20 + imgTileH + 14 + captionH + 18 + tableCardH + 20 + summaryH + 18 + footH + 160

	cv, err := newReportCanvas(totalH)
	if err != nil {
		return nil, err
	}
	defer cv.close()
	draw.Draw(cv.img, cv.img.Bounds(), &image.Uniform{icCanvas.rgba()}, image.Point{}, draw.Src)

	weekday := reportWeekdays[now.Format("Monday")]
	innerW := imgW - imgPad*2
	rate := pct(r.Qualified, r.TotalUsers)

	// 1) 头部横幅：标题 + 日期，右侧整体达标率
	bx, by := imgPad, imgPad
	cv.fillRoundRect(bx, by, innerW, bannerH, cardRadius, icBanner)
	cv.text(bx+36, by+72, r.TenantName+" · 使用日报", 42, true, icOnDark)
	cv.text(bx+36, by+118, fmt.Sprintf("%s（%s）", r.Date, weekday), 25, false, icOnDarkSub)
	cv.textRight(bx+innerW-36, by+76, rate, 50, true, icOnDark)
	cv.textRight(bx+innerW-36, by+118, "整体达标率", 23, false, icOnDarkSub)
	y := by + bannerH + 20

	// 2) 四联统计瓦片
	tileW := (innerW - tileGap*3) / 4
	msgDelta := deltaPercent(r.TotalMessages, r.PrevMessages)
	msgSubCol := icMuted
	switch {
	case strings.HasPrefix(msgDelta, "-"):
		msgSubCol = icOrange
	case msgDelta != "持平":
		msgSubCol = icBlue // 蓝色表示消息量变化，避免与达标绿混淆
	}
	cv.statTile(bx, y, tileW, icBlue, icBlueBg, "总用户数", fmt.Sprintf("%d", r.TotalUsers), "", icMuted)
	cv.statTile(bx+tileW+tileGap, y, tileW, icGreen, icGreenBg, "达标用户", fmt.Sprintf("%d", r.Qualified), "达标率 "+pct(r.Qualified, r.TotalUsers), icGreen)
	cv.statTile(bx+(tileW+tileGap)*2, y, tileW, icOrange, icOrangeBg, "未达标用户", fmt.Sprintf("%d", r.Unqualified), "未达标率 "+pct(r.Unqualified, r.TotalUsers), icOrange)
	cv.statTile(bx+(tileW+tileGap)*3, y, tileW, icSlate, icSlateBg, "昨日消息", fmt.Sprintf("%d", r.TotalMessages), "较前日 "+msgDelta, msgSubCol)
	y += imgTileH + 14

	// 3) 达标标准说明
	cv.text(bx+6, y+32, "达标标准：登录 ≥ 1 次 且 对话 ≥ 2 次", 24, false, icMuted)
	y += captionH + 18

	// 4) 明细表卡片
	cv.fillRoundRect(bx, y, innerW, tableCardH, cardRadius, icCard)
	tx := bx + 24
	tw := innerW - 48
	// 列中心按最坏字形宽度预留：状态胶囊（未达标≈140px）与最长
	// 时间串（≈200px）之间保持 ≥20px 间隙。
	colLogin := tx + 428       // 登录列中心
	colChat := colLogin + 100  // 对话列中心
	colStatus := colChat + 116 // 状态列中心
	activeRight := tx + tw     // 上次活跃右缘
	// 表头（浅色圆角条）
	cv.fillRoundRect(tx, y+24, tw, imgTableHeadH, 12, icHeadBg)
	hh := y + 24 + imgTableHeadH/2 + 9
	cv.text(tx+10, hh, "用户", 25, true, icSecondary)
	cv.textCenter(colLogin, hh, "登录", 25, true, icSecondary)
	cv.textCenter(colChat, hh, "对话", 25, true, icSecondary)
	cv.textCenter(colStatus, hh, "状态", 25, true, icSecondary)
	cv.textRight(activeRight, hh, "上次活跃", 25, true, icSecondary)
	rowY := y + 24 + imgTableHeadH + 8
	if len(rows) == 0 {
		cv.textCenter(tx+tw/2, rowY+34, "昨日无活跃用户", 26, false, icMuted)
		rowY += imgRowH
	}
	for i, row := range rows {
		base := rowY + imgRowH/2 + 10
		cv.text(tx+10, base, cv.truncateMiddle(reportDisplayName(row.Username), 380, 27, false), 27, false, icPrimary)
		cv.textCenter(colLogin, base, fmt.Sprintf("%d", row.Logins), 27, false, icSecondary)
		cv.textCenter(colChat, base, fmt.Sprintf("%d", row.Chats), 27, false, icSecondary)
		if row.Qualified {
			cv.pill(colStatus, rowY+imgRowH/2, "达标", icGreenBg, icGreen)
		} else {
			cv.pill(colStatus, rowY+imgRowH/2, "未达标", icOrangeBg, icOrange)
		}
		cv.textRight(activeRight, base, lastActiveText(row.LastActive, now), 24, false, icMuted)
		if i < len(rows)-1 || hidden > 0 {
			cv.hline(tx+10, activeRight, rowY+imgRowH, icDivider)
		}
		rowY += imgRowH
	}
	if hidden > 0 {
		cv.text(tx+10, rowY+34, fmt.Sprintf("其余 %d 人略", hidden), 24, false, icMuted)
		rowY += 46
	}
	y += tableCardH + 20

	// 5) 小结卡片：达标率进度条 + 环比趋势
	// 达标率 ≥30% 进度条用绿色，低于则橙色提示改进空间。
	rateCol := icGreen
	if ratePermille(r.Qualified, r.TotalUsers) < 300 {
		rateCol = icOrange
	}
	cv.fillRoundRect(bx, y, innerW, summaryH, cardRadius, icCard)
	cv.text(bx+32, y+52, "整体达标率", 25, false, icSecondary)
	cv.textRight(bx+innerW-32, y+56, rate, 38, true, rateCol)
	barY := y + 82
	barW := innerW - 64
	cv.fillRoundRect(bx+32, barY, barW, 16, 8, icTrack)
	perm := ratePermille(r.Qualified, r.TotalUsers)
	fillW := barW * perm / 1000
	if fillW > 8 {
		cv.fillRoundRect(bx+32, barY, fillW, 16, 8, rateCol)
	}
	prevPerm := ratePermille(r.PrevQualified, r.TotalUsers)
	diffPP := float64(perm-prevPerm) / 10.0
	trend, trendCol := "较前日持平", icSecondary
	switch {
	case perm > prevPerm:
		trend = fmt.Sprintf("较前日 +%.1f 个百分点，趋势向好", diffPP)
		trendCol = icGreen
	case perm < prevPerm:
		trend = fmt.Sprintf("较前日 %.1f 个百分点，有所回落", diffPP)
		trendCol = icOrange
	}
	cv.text(bx+32, y+144, trend, 25, false, trendCol)
	y += summaryH + 18

	// 6) 页脚
	cv.textCenter(imgW/2, y+34, "报告生成于 "+now.Format("2006-01-02 15:04"), 22, false, icMuted)
	finalH := y + footH
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

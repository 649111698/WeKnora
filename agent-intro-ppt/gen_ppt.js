/* WeKnora 智能体介绍 PPT 生成脚本 */
const pptxgen = require("pptxgenjs");
const P = new pptxgen();
P.layout = "LAYOUT_WIDE";
P.author = "WeKnora";
P.title = "智能体 · 企业智能体平台介绍";

/* ---------- 设计常量 ---------- */
const W = 13.33, H = 7.5, M = 0.55;
const FONT = "微软雅黑";
const INK = "0B2924";      // 深墨绿(暗底)
const INK2 = "123B31";     // 暗底面板
const PANEL = "0D6B50";    // 主色上的面板
const PRIMARY = "0E7A5A";  // 主色 深翡翠绿
const ACCENT = "1FBF8F";   // 强调 薄荷绿
const TEXT = "1B2B26";
const MUTED = "5F7A72";
const LINEC = "DFEAE5";    // 浅 hairline
const TINT = "EAF4EF";     // 浅绿底纹
const TINT2 = "F5FAF8";
const WHITE = "FFFFFF";
const MINT = "E3F6EE";     // 暗底/主色上的浅字
const EDGE = "2E7A63";     // 暗底描边

const t = (s, txt, o) => s.addText(txt, Object.assign({ fontFace: FONT, margin: 0 }, o));
const sh = () => ({ type: "outer", color: "1B2B26", blur: 7, offset: 2, angle: 90, opacity: 0.13 });
const rrect = (s, x, y, w, h, fill, line, radius, shadow) =>
  s.addShape(P.shapes.ROUNDED_RECTANGLE, Object.assign(
    { x, y, w, h, rectRadius: radius, fill: { color: fill } },
    line ? { line } : {},
    shadow ? { shadow: sh() } : {}));
const hline = (s, x, y, w, c = LINEC, wt = 0.75) =>
  s.addShape(P.shapes.LINE, { x, y, w, h: 0, line: { color: c, width: wt } });
const vline = (s, x, y, h, c = LINEC, wt = 0.75) =>
  s.addShape(P.shapes.LINE, { x, y, w: 0, h, line: { color: c, width: wt } });
const arrow = (s, x, y, w, h, o = {}) =>
  s.addShape(P.shapes.LINE, Object.assign({ x, y, w, h, line: Object.assign({ color: PRIMARY, width: 1.75 }, o.line || {}) }, o.begin !== undefined ? { line: Object.assign({ color: PRIMARY, width: 1.75, beginArrowType: "triangle" }, o.line || {}) } : {}, o.end !== undefined && !o.begin ? { line: Object.assign({ color: PRIMARY, width: 1.75, endArrowType: "triangle" }, o.line || {}) } : {}));
const dot = (s, x, y, d = 0.09, c = ACCENT) =>
  s.addShape(P.shapes.OVAL, { x, y, w: d, h: d, fill: { color: c }, line: { type: "none" } });

const cjkW = (txt, pt) => {
  let w = 0;
  for (const ch of txt) w += /[\x00-\xff]/.test(ch) ? pt * 0.56 : pt;
  return w / 72;
};
const chip = (s, x, y, txt, o = {}) => {
  const pt = o.pt || 10.5, h = o.h || 0.4;
  const w = o.w || cjkW(txt, pt) + 0.3;
  s.addShape(P.shapes.ROUNDED_RECTANGLE, {
    x, y, w, h, rectRadius: Math.min(0.07, h / 2),
    fill: { color: o.fill || WHITE },
    line: o.line === null ? { type: "none" } : (o.line || { color: LINEC, width: 1 }),
  });
  t(s, txt, { x, y, w, h, pt, align: "center", valign: "middle", color: o.color || TEXT, bold: !!o.bold, fontSize: pt });
  return w;
};

let pageNo = 0;
const header = (s, kicker, title) => {
  pageNo += 1;
  t(s, kicker, { x: M, y: 0.36, w: 8, h: 0.3, fontSize: 11, bold: true, color: PRIMARY, charSpacing: 2 });
  t(s, title, { x: M, y: 0.66, w: 12.2, h: 0.62, fontSize: 27, bold: true, color: TEXT });
  t(s, String(pageNo).padStart(2, "0"), { x: 12.35, y: 7.06, w: 0.45, h: 0.3, fontSize: 10, color: MUTED, align: "right" });
};

/* 暗底节点网络 motif(呼应"关系图") */
const netMotif = (s, nodes, edges, o = {}) => {
  const cc = o.line || "1D6B55";
  edges.forEach(([a, b]) => {
    const [x1, y1] = nodes[a], [x2, y2] = nodes[b];
    s.addShape(P.shapes.LINE, {
      x: Math.min(x1, x2), y: Math.min(y1, y2),
      w: Math.abs(x2 - x1), h: Math.abs(y2 - y1),
      line: { color: cc, width: 1 },
      flipH: (x2 - x1) * (y2 - y1) < 0,
    });
  });
  nodes.forEach(([cx, cy, r, hub]) => {
    s.addShape(P.shapes.OVAL, {
      x: cx - r, y: cy - r, w: r * 2, h: r * 2,
      fill: { color: ACCENT, transparency: hub ? 0 : 55 }, line: { type: "none" },
    });
    if (hub) s.addShape(P.shapes.OVAL, {
      x: cx - r - 0.16, y: cy - r - 0.16, w: r * 2 + 0.32, h: r * 2 + 0.32,
      fill: { color: o.bg || INK }, line: { color: ACCENT, width: 1.25 },
    });
  });
};

/* ============ S1 封面 ============ */
{
  const s = P.addSlide();
  s.background = { color: INK };
  const nodes = [
    [9.05, 1.30, 0.11], [10.15, 0.95, 0.16], [11.35, 1.55, 0.09],
    [10.75, 2.55, 0.22, 1], [9.35, 2.95, 0.12], [12.15, 2.95, 0.12],
    [11.85, 4.15, 0.16], [9.95, 4.35, 0.10], [12.55, 5.15, 0.11],
    [10.65, 5.55, 0.15], [9.15, 5.75, 0.09], [11.35, 6.45, 0.12], [8.55, 4.15, 0.08],
  ];
  const edges = [[0, 1], [1, 2], [2, 3], [3, 4], [3, 5], [5, 6], [6, 7], [7, 3],
    [6, 8], [8, 9], [9, 10], [9, 11], [7, 12], [4, 12], [0, 4]];
  netMotif(s, nodes, edges);
  t(s, "WeKnora · 腾讯开源企业级知识管理与智能体平台", { x: 0.8, y: 1.35, w: 8, h: 0.35, fontSize: 13, bold: true, color: ACCENT, charSpacing: 1.5 });
  t(s, "智能体", { x: 0.75, y: 1.85, w: 7, h: 1.5, fontSize: 76, bold: true, color: WHITE });
  t(s, "A I   A G E N T", { x: 0.8, y: 3.45, w: 6, h: 0.35, fontSize: 13, bold: true, color: ACCENT, charSpacing: 6 });
  t(s, "大模型驱动的「数字员工」：\n自主规划 · 调用工具 · 完成企业里的真实任务", { x: 0.8, y: 4.05, w: 7.1, h: 1.3, fontSize: 19, color: MINT, lineSpacingMultiple: 1.25 });
  const chipTxts = ["自主规划", "工具执行", "知识检索", "MCP 连接"];
  let cx = 0.8;
  chipTxts.forEach((c) => { cx += chip(s, cx, 5.6, c, { fill: INK2, line: { color: EDGE, width: 1 }, color: MINT, pt: 11.5, h: 0.5 }) + 0.22; });
  t(s, "企业智能体专题分享 · 2026 年 9 月", { x: 0.8, y: 6.85, w: 6, h: 0.3, fontSize: 11, color: "6E9488" });
  s.addNotes("开场:今天用 30 分钟讲清楚智能体是什么、能为企业做什么、怎么集成进来。所有能力均以 WeKnora 平台为例。");
}

/* ============ S2 议程 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "AGENDA", "本次分享");
  const rows = [
    ["01", "什么是智能体", "定义 · 四大构件 · 从聊天机器人到智能体的演进"],
    ["02", "智能体能做什么", "双引擎模式 · 30+ 内置工具 · 企业典型场景"],
    ["03", "企业收益", "效率 · 知识资产 · 全渠道触达 · 自主可控"],
    ["04", "企业如何集成", "四条接入路径 · 五步上线 · 安全与治理"],
    ["05", "关系图与 MCP 中台", "平台分层架构 · 双向 MCP · 工具中台模式"],
  ];
  rows.forEach((r, i) => {
    const y = 1.72 + i * 0.99;
    t(s, r[0], { x: M, y: y + 0.04, w: 0.85, h: 0.5, fontSize: 25, bold: true, color: PRIMARY });
    t(s, r[1], { x: 1.55, y, w: 6.5, h: 0.38, fontSize: 17.5, bold: true, color: TEXT });
    t(s, r[2], { x: 1.55, y: y + 0.4, w: 6.6, h: 0.32, fontSize: 12, color: MUTED });
    if (i < 4) hline(s, 1.55, y + 0.86, 6.6);
  });
  rrect(s, 8.6, 1.72, 4.18, 4.95, TINT, null, 0.09);
  t(s, "一句话预告", { x: 8.95, y: 2.05, w: 3.5, h: 0.3, fontSize: 11.5, bold: true, color: PRIMARY, charSpacing: 2 });
  t(s, "智能体\n= 大模型 × 记忆\n× 工具 × 规划", { x: 8.95, y: 2.55, w: 3.5, h: 1.9, fontSize: 21, bold: true, color: PRIMARY, lineSpacingMultiple: 1.3 });
  t(s, "它不是「回答一段话」，而是「完成一件事」——给一个目标，还一个结果。", { x: 8.95, y: 4.6, w: 3.5, h: 1.2, fontSize: 12.5, color: TEXT, lineSpacingMultiple: 1.35 });
  dot(s, 8.95, 6.15, 0.1); dot(s, 9.25, 6.15, 0.1); dot(s, 9.55, 6.15, 0.1);
  s.addNotes("五个部分,先建立认知,再看能力与收益,最后落到怎么接入和平台架构。");
}

/* ============ S3 什么是智能体 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "01 · 认识智能体", "什么是智能体（AI Agent）");
  t(s, "以大模型为「大脑」，能够自主感知、规划并调用工具完成多步任务的 AI 系统。",
    { x: M, y: 1.48, w: 12.2, h: 0.42, fontSize: 18, bold: true, color: TEXT });
  t(s, "它不是一问一答，而是「给一个目标，还一个结果」。",
    { x: M, y: 1.95, w: 12.2, h: 0.35, fontSize: 13, color: MUTED });
  const cards = [
    ["大脑 · LLM", ["理解与推理", "生成与反思", "20+ 模型可插拔"]],
    ["记忆 · Memory", ["对话上下文", "五类长期记忆", "跨会话记住偏好"]],
    ["工具 · Tools", ["知识检索 · SQL", "代码沙箱 · MCP", "与世界交互的双手"]],
    ["规划 · Planning", ["任务分解 todo", "序列思考", "自我修正 · 重试"]],
  ];
  const y0 = 2.62, cw = 2.18, ch = 2.5, gap = 0.42;
  cards.forEach((c, i) => {
    const x = M + i * (cw + gap);
    rrect(s, x, y0, cw, ch, TINT2, { color: LINEC, width: 1 }, 0.07);
    dot(s, x + 0.22, y0 + 0.28, 0.11, PRIMARY);
    t(s, c[0], { x: x + 0.22, y: y0 + 0.5, w: cw - 0.4, h: 0.34, fontSize: 15, bold: true, color: TEXT });
    t(s, c[1].map((l, j) => ({ text: l, options: { breakLine: true } })),
      { x: x + 0.22, y: y0 + 0.98, w: cw - 0.38, h: 1.4, fontSize: 11.5, color: MUTED, paraSpaceAfter: 6 });
    if (i < 3) t(s, "+", { x: x + cw + 0.02, y: y0 + ch / 2 - 0.3, w: gap - 0.04, h: 0.6, fontSize: 24, bold: true, color: PRIMARY, align: "center", valign: "middle" });
  });
  const ex = M + 4 * (cw + gap);
  t(s, "=", { x: ex - 0.4, y: y0 + ch / 2 - 0.3, w: 0.4, h: 0.6, fontSize: 24, bold: true, color: PRIMARY, align: "center", valign: "middle" });
  rrect(s, ex, y0, 1.82, ch, INK, null, 0.07);
  t(s, "智能体\nAI AGENT", { x: ex + 0.1, y: y0 + 0.55, w: 1.62, h: 1.0, fontSize: 19, bold: true, color: WHITE, align: "center", lineSpacingMultiple: 1.2 });
  t(s, "给目标 · 还结果", { x: ex + 0.1, y: y0 + 1.7, w: 1.62, h: 0.3, fontSize: 11, color: ACCENT, align: "center" });
  chip(s, 2.0, 5.62, "聊天机器人 · 回答一段话", { fill: TINT2, pt: 13, h: 0.62, w: 3.4, color: MUTED });
  arrow(s, 5.5, 5.93, 0.6, 0, { end: true });
  chip(s, 6.2, 5.62, "智能体 · 完成一件事", { fill: PRIMARY, pt: 13, h: 0.62, w: 3.4, color: WHITE, bold: true, line: null });
  t(s, "智能体会自己查资料、写代码、调系统、核对结果，再交付答案。",
    { x: M, y: 6.55, w: 12.2, h: 0.32, fontSize: 12, color: MUTED, align: "center" });
  s.addNotes("四大构件公式是全片主线;底部对比一句话点出与聊天机器人的本质差异。");
}

/* ============ S4 三代演进 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "01 · 认识智能体", "企业问答的三代演进");
  const data = [
    ["第一代", "规则问答机器人", ["关键词匹配 · 流程树", "固定话术应答"], "局限：换个问法就失灵", false],
    ["第二代", "检索增强 RAG", ["大模型 + 知识库检索", "回答有出处、可溯源"], "局限：只能「回答」，不能「动手」", false],
    ["第三代", "自主智能体 Agent", ["自主规划 + 工具执行", "反思修正 · 跨系统协作"], "跃迁：直接「完成任务」", true],
  ];
  const y0 = 1.62, cw = 3.85, ch = 3.95, gap = 0.29;
  data.forEach((d, i) => {
    const x = M + i * (cw + gap);
    const hot = d[4];
    rrect(s, x, y0, cw, ch, hot ? PRIMARY : TINT2, hot ? null : { color: LINEC, width: 1 }, 0.08);
    t(s, d[0], { x: x + 0.3, y: y0 + 0.28, w: 2, h: 0.28, fontSize: 11, bold: true, color: hot ? ACCENT : PRIMARY, charSpacing: 2 });
    t(s, d[1], { x: x + 0.3, y: y0 + 0.6, w: cw - 0.6, h: 0.42, fontSize: 18, bold: true, color: hot ? WHITE : TEXT });
    hline(s, x + 0.3, y0 + 1.18, cw - 0.6, hot ? "2E7A63" : LINEC);
    t(s, d[2].map((l, j) => ({ text: l, options: { breakLine: true } })),
      { x: x + 0.3, y: y0 + 1.42, w: cw - 0.6, h: 1.1, fontSize: 12.5, color: hot ? MINT : MUTED, paraSpaceAfter: 8 });
    t(s, d[3], { x: x + 0.3, y: y0 + 3.2, w: cw - 0.6, h: 0.55, fontSize: 12.5, bold: true, color: hot ? "9FE8D2" : MUTED });
    if (i < 2) arrow(s, x + cw + 0.02, y0 + ch / 2, gap - 0.05, 0, { end: true });
  });
  s.addShape(P.shapes.RECTANGLE, { x: 0, y: 6.02, w: W, h: 0.88, fill: { color: TINT }, line: { type: "none" } });
  t(s, "跃迁点：从「回答问题」到「完成任务」—— WeKnora 同时提供两种模式，按场景自由切换",
    { x: M, y: 6.02, w: W - 2 * M, h: 0.88, fontSize: 15, bold: true, color: PRIMARY, align: "center", valign: "middle" });
  s.addNotes("RAG 是智能体的子能力;WeKnora 把两种模式都开放,简单问题不浪费算力,复杂任务才动用智能体。");
}

/* ============ S5 双引擎 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "02 · 智能体能做什么", "两种引擎：快速问答 与 智能推理");
  /* 左卡 */
  rrect(s, M, 1.55, 6.03, 4.85, TINT2, { color: LINEC, width: 1 }, 0.08);
  t(s, "QUICK ANSWER", { x: 0.85, y: 1.82, w: 4, h: 0.26, fontSize: 10.5, bold: true, color: PRIMARY, charSpacing: 2 });
  t(s, "快速问答（RAG 管道）", { x: 0.85, y: 2.1, w: 5.4, h: 0.42, fontSize: 19, bold: true, color: TEXT });
  const fchips = [["用户提问", 1.5], ["检索 + 重排", 1.62], ["生成带引用回答", 1.95]];
  let fx = 0.85;
  fchips.forEach((c, i) => {
    chip(s, fx, 2.78, c[0], { pt: 10.5, h: 0.52, w: c[1], color: TEXT });
    fx += c[1];
    if (i < 2) { arrow(s, fx + 0.03, 3.04, 0.24, 0, { end: true }); fx += 0.3; }
  });
  const lrows = ["秒级响应，适合高频、标准化的问答", "答案附引用，可溯源到原文档段落", "智能体亦可退化为此模式承接高并发"];
  lrows.forEach((r, i) => {
    dot(s, 0.88, 3.85 + i * 0.62 + 0.06, 0.09, PRIMARY);
    t(s, r, { x: 1.12, y: 3.85 + i * 0.62, w: 5.2, h: 0.4, fontSize: 12.5, color: TEXT });
  });
  /* 右卡 */
  rrect(s, 6.75, 1.55, 6.03, 4.85, INK, null, 0.08);
  t(s, "SMART REASONING", { x: 7.05, y: 1.82, w: 4, h: 0.26, fontSize: 10.5, bold: true, color: ACCENT, charSpacing: 2 });
  t(s, "智能推理（ReAct 智能体）", { x: 7.05, y: 2.1, w: 5.4, h: 0.42, fontSize: 19, bold: true, color: WHITE });
  const loop = ["思考", "分析", "行动", "观察"];
  let lx = 7.08;
  loop.forEach((c, i) => {
    chip(s, lx, 2.78, c, { fill: INK2, line: { color: EDGE, width: 1 }, color: WHITE, pt: 12, h: 0.52, w: 1.0, bold: true });
    lx += 1.0;
    if (i < 3) { arrow(s, lx + 0.06, 3.04, 0.26, 0, { end: true, line: { color: ACCENT } }); lx += 0.38; }
  });
  s.addShape(P.shapes.LINE, { x: 7.58, y: 3.62, w: 4.5, h: 0, line: { color: ACCENT, width: 1, dashType: "dash", beginArrowType: "triangle" } });
  t(s, "循环，直到任务完成", { x: 7.58, y: 3.74, w: 4.5, h: 0.28, fontSize: 10, color: ACCENT, align: "center" });
  const rrows = ["默认最多 20 轮迭代（可调至 100）", "支持一轮内并行调用多个工具", "200K token 上下文预算，自动压缩记忆", "结果自我核对，失败自动重试"];
  rrows.forEach((r, i) => {
    dot(s, 7.08, 4.28 + i * 0.52 + 0.06);
    t(s, r, { x: 7.32, y: 4.28 + i * 0.52, w: 5.3, h: 0.38, fontSize: 12.5, color: MINT });
  });
  t(s, "同一知识库，两种模式按需切换 —— 高频简单问题走快速问答，复杂多步任务交给智能推理。",
    { x: M, y: 6.68, w: 12.2, h: 0.35, fontSize: 12, color: MUTED });
  s.addNotes("左:RAG 管道,快而稳;右:ReAct 循环 think-analyze-act-observe,是智能体的心脏。WeKnora 一键切换。");
}

/* ============ S6 内置工具 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "02 · 智能体能做什么", "开箱即用：30+ 内置工具，六大能力族");
  t(s, "30+", { x: M, y: 1.85, w: 3.6, h: 1.5, fontSize: 84, bold: true, color: PRIMARY });
  t(s, "内置工具", { x: M, y: 3.5, w: 3.4, h: 0.4, fontSize: 19, bold: true, color: TEXT });
  t(s, "开箱即用，智能体按需自动调用", { x: M, y: 3.95, w: 3.5, h: 0.35, fontSize: 12, color: MUTED });
  hline(s, M, 4.55, 3.1);
  const minis = ["11 家联网搜索引擎", "5 类长期记忆", "20 轮最大迭代（默认）"];
  minis.forEach((m, i) => {
    dot(s, M, 4.8 + i * 0.55, 0.09, ACCENT);
    t(s, m, { x: M + 0.24, y: 4.73 + i * 0.55, w: 3.2, h: 0.35, fontSize: 12, color: TEXT });
  });
  const rows = [
    ["知识检索", "语义搜索 · 正则匹配 · GraphRAG 知识图谱"],
    ["数据分析", "只读 SQL 查询，DuckDB 直接分析 CSV / Excel"],
    ["联网搜索", "11 家搜索引擎，安全抓取网页并自动摘要"],
    ["代码沙箱", "Docker / E2B / Cube 隔离执行，会话级环境"],
    ["技能系统", "SKILL.md 渐进加载，可从技能市场一键安装"],
    ["Wiki 工程", "10 个工具自动维护企业 Wiki 与知识图谱"],
  ];
  rows.forEach((r, i) => {
    const y = 1.7 + i * 0.87;
    s.addShape(P.shapes.ROUNDED_RECTANGLE, { x: 4.5, y: y + 0.1, w: 0.26, h: 0.26, rectRadius: 0.05, fill: { color: TINT }, line: { type: "none" } });
    t(s, r[0], { x: 4.92, y: y + 0.02, w: 1.75, h: 0.4, fontSize: 14.5, bold: true, color: TEXT });
    t(s, r[1], { x: 6.75, y: y + 0.06, w: 6.0, h: 0.36, fontSize: 12, color: MUTED });
    if (i < 5) hline(s, 4.5, y + 0.72, 8.28);
  });
  s.addNotes("六大能力族覆盖企业 90% 场景;工具白名单在智能体配置里逐项开关。");
}

/* ============ S7 企业场景 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "02 · 智能体能做什么", "企业里的四个典型智能体");
  const data = [
    ["场景 01", "知识", "员工知识助手", "绑定企业知识库，在企微 / 飞书里 @ 它即问即答；答案带原文引用，新员工上手快。", "知识检索 · 引用溯源"],
    ["场景 02", "数据", "数据分析师", "自然语言查数据库、分析 Excel；在沙箱里写代码跑数，直接给出结论与图表建议。", "只读 SQL · DuckDB · 沙箱"],
    ["场景 03", "维基", "Wiki 工程师", "文档入库自动生成带知识图谱的 Wiki；发现过期、矛盾内容自动标记 issue。", "10 个 Wiki 工具 · 自动蒸馏"],
    ["场景 04", "执行", "跨系统执行者", "通过 MCP 连接 OA / CRM / 工单系统：查订单、提审批、开工单；高危操作人工确认。", "MCP 工具 · 人工审批"],
  ];
  const y0 = 1.62, cw = 2.85, gap = 0.31, ch = 4.45;
  data.forEach((d, i) => {
    const x = 0.5 + i * (cw + gap);
    rrect(s, x, y0, cw, ch, WHITE, { color: LINEC, width: 1 }, 0.08, true);
    t(s, d[0], { x: x + 0.26, y: y0 + 0.24, w: 1.6, h: 0.26, fontSize: 10.5, bold: true, color: PRIMARY, charSpacing: 1.5 });
    s.addShape(P.shapes.OVAL, { x: x + 0.26, y: y0 + 0.62, w: 0.56, h: 0.56, fill: { color: TINT }, line: { type: "none" } });
    t(s, d[1], { x: x + 0.26, y: y0 + 0.62, w: 0.56, h: 0.56, fontSize: 12.5, bold: true, color: PRIMARY, align: "center", valign: "middle" });
    t(s, d[2], { x: x + 0.26, y: y0 + 1.38, w: cw - 0.5, h: 0.4, fontSize: 16, bold: true, color: TEXT });
    t(s, d[3], { x: x + 0.26, y: y0 + 1.88, w: cw - 0.5, h: 1.85, fontSize: 11.5, color: MUTED, lineSpacingMultiple: 1.25 });
    t(s, "代表能力 · " + d[4], { x: x + 0.26, y: y0 + 3.85, w: cw - 0.5, h: 0.5, fontSize: 10.5, bold: true, color: PRIMARY });
  });
  t(s, "每一个都是可配置的「自定义智能体」：选模型、绑知识库、设工具白名单，复制即得。",
    { x: M, y: 6.45, w: 12.2, h: 0.35, fontSize: 12, color: MUTED, align: "center" });
  s.addNotes("四类角色对应四类买点:员工服务、经营分析、知识运营、流程自动化。");
}

/* ============ S8 企业收益 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "03 · 企业收益", "智能体进入企业，带来四层收益");
  const stats = [
    ["7×24", "效率提升", "多步任务自主执行，20 轮迭代 + 并行工具调用，人工只处理例外。"],
    ["13", "知识资产沉淀", "种文档格式自动入库成知识库，再由智能体蒸馏为带图谱的 Wiki。"],
    ["10", "全渠道触达", "个 IM 渠道一键绑定（企微 / 飞书 / 钉钉…），客户在哪服务在哪。"],
    ["20+", "自主可控", "家大模型可插拔（DeepSeek / 混元 / Qwen…），8 种向量库，可全私有化。"],
  ];
  const y0 = 1.95, cw = 2.85, gap = 0.31;
  stats.forEach((d, i) => {
    const x = 0.5 + i * (cw + gap);
    t(s, d[0], { x, y: y0, w: cw, h: 1.0, fontSize: 46, bold: true, color: PRIMARY });
    t(s, d[1], { x, y: y0 + 1.12, w: cw, h: 0.36, fontSize: 15.5, bold: true, color: TEXT });
    t(s, d[2], { x, y: y0 + 1.56, w: cw - 0.15, h: 1.7, fontSize: 11.5, color: MUTED, lineSpacingMultiple: 1.3 });
    if (i < 3) vline(s, x + cw + gap / 2, y0 + 0.15, 2.9);
  });
  s.addShape(P.shapes.RECTANGLE, { x: 0, y: 5.95, w: W, h: 0.95, fill: { color: TINT }, line: { type: "none" } });
  t(s, "本质：把组织里的「隐性知识」变成「随取随用的执行力」—— 知识不再只是存起来，而是动起来。",
    { x: M, y: 5.95, w: W - 2 * M, h: 0.95, fontSize: 15.5, bold: true, color: PRIMARY, align: "center", valign: "middle" });
  t(s, "注：数字为 WeKnora v0.8 平台内置能力统计", { x: M, y: 7.02, w: 6, h: 0.28, fontSize: 10, color: MUTED });
  s.addNotes("四层收益对应四类决策人:CIO 看可控,COO 看效率,CKO 看知识资产,CMO 看触达。");
}

/* ============ S9 四条接入路径 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "04 · 企业如何集成", "四条接入路径，覆盖所有触点");
  const data = [
    ["1", "IM 渠道绑定", "企微 / 飞书 / 钉钉 / Slack 等 10 个平台；智能体配置页直接绑定渠道，员工在聊天窗口 @ 它即用。", "无需开发 · 配置即生效"],
    ["2", "网页挂件 Embed", "官网、帮助中心、内网门户嵌入对话窗口，匿名访客也能用；对外服务不打扰内部系统。", "域名白名单 · 安全 token · 限流"],
    ["3", "REST API", "约 360 个端点覆盖全部平台能力；agent-chat 接口 SSE 流式输出思考与执行过程。", "Scoped API Key 按能力授权"],
    ["4", "MCP 协议", "官方包 tencent-weknora-mcp 一行命令接入，把 30+ 知识工具开放给外部智能体。", "Claude · Cursor 等可直接调用"],
  ];
  const y0 = 1.6, cw = 2.85, gap = 0.31, ch = 4.35;
  data.forEach((d, i) => {
    const x = 0.5 + i * (cw + gap);
    rrect(s, x, y0, cw, ch, i === 3 ? TINT : TINT2, { color: i === 3 ? "BFDFD2" : LINEC, width: 1 }, 0.08);
    s.addShape(P.shapes.OVAL, { x: x + 0.26, y: y0 + 0.26, w: 0.55, h: 0.55, fill: { color: PRIMARY }, line: { type: "none" } });
    t(s, d[0], { x: x + 0.26, y: y0 + 0.26, w: 0.55, h: 0.55, fontSize: 17, bold: true, color: WHITE, align: "center", valign: "middle" });
    t(s, d[1], { x: x + 0.26, y: y0 + 1.0, w: cw - 0.5, h: 0.4, fontSize: 15.5, bold: true, color: TEXT });
    t(s, d[2], { x: x + 0.26, y: y0 + 1.5, w: cw - 0.5, h: 2.0, fontSize: 11.5, color: MUTED, lineSpacingMultiple: 1.25 });
    t(s, d[3], { x: x + 0.26, y: y0 + 3.62, w: cw - 0.5, h: 0.6, fontSize: 10.5, bold: true, color: PRIMARY });
  });
  s.addShape(P.shapes.RECTANGLE, { x: 0, y: 6.3, w: W, h: 0.8, fill: { color: TINT }, line: { type: "none" } });
  t(s, "四条路径可同时启用 —— 对内 IM · 对外网页 · 系统间 API · 生态 MCP",
    { x: M, y: 6.3, w: W - 2 * M, h: 0.8, fontSize: 14.5, bold: true, color: PRIMARY, align: "center", valign: "middle" });
  s.addNotes("接入形态按受众选:员工走 IM,客户走挂件,系统走 API,生态走 MCP。");
}

/* ============ S10 五步上线 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "04 · 企业如何集成", "从零到一：五步上线第一个智能体");
  const steps = [
    ["接入知识", "13 种格式文档上传；飞书 / Notion / GitLab / 语雀 / RSS 自动同步"],
    ["配置智能体", "选模型与温度，绑定知识库（全部 / 指定），设工具白名单"],
    ["连接工具", "注册 MCP 服务、安装技能、配沙箱；关键工具开启人工审批"],
    ["上线渠道", "绑定 IM、嵌入网页挂件，或直接对外开放 API"],
    ["观测治理", "Langfuse 追踪每轮思考与工具调用；审计日志，持续调优"],
  ];
  const xs = [1.9, 4.15, 6.4, 8.65, 10.9], ly = 3.62;
  s.addShape(P.shapes.LINE, { x: 1.6, y: ly, w: 9.6, h: 0, line: { color: "BFE0D4", width: 2.5 } });
  steps.forEach((st, i) => {
    const cxr = xs[i];
    s.addShape(P.shapes.OVAL, { x: cxr - 0.29, y: ly - 0.29, w: 0.58, h: 0.58, fill: { color: PRIMARY }, line: { color: WHITE, width: 2 } });
    t(s, String(i + 1), { x: cxr - 0.29, y: ly - 0.29, w: 0.58, h: 0.58, fontSize: 16, bold: true, color: WHITE, align: "center", valign: "middle" });
    const bx = Math.max(M, Math.min(cxr - 1.375, 12.78 - 2.75));
    const above = i % 2 === 0;
    t(s, [
      { text: st[0], options: { fontSize: 15, bold: true, color: TEXT, breakLine: true } },
      { text: st[1], options: { fontSize: 11.5, color: MUTED } },
    ], { x: bx, y: above ? 1.5 : 4.05, w: 2.75, h: 1.75, valign: above ? "bottom" : "top", lineSpacingMultiple: 1.2, paraSpaceAfter: 6 });
    if (above) vline(s, cxr, 3.33, 0.16, "BFE0D4", 1.5); else vline(s, cxr, 3.76, 0.16, "BFE0D4", 1.5);
  });
  t(s, "主要配置均可在管理界面完成，无需写代码；MCP 服务与技能同样界面化接入。",
    { x: M, y: 6.35, w: 12.2, h: 0.35, fontSize: 12, color: MUTED, align: "center" });
  s.addNotes("五步通常一个下午可完成最小闭环:一个知识库 + 一个智能体 + 一个 IM 渠道。");
}

/* ============ S11 安全与治理 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "04 · 企业如何集成", "安全与治理：敢把权限交给智能体");
  rrect(s, M, 1.6, 5.2, 5.0, INK, null, 0.09);
  t(s, "把权限管到「工具级」，把执行留痕到「每一步」。", { x: 0.9, y: 2.05, w: 4.5, h: 1.9, fontSize: 21, bold: true, color: WHITE, lineSpacingMultiple: 1.35 });
  t(s, "WeKnora 的治理体系，让企业放心把系统权限交给智能体。", { x: 0.9, y: 4.15, w: 4.4, h: 0.9, fontSize: 12.5, color: MINT, lineSpacingMultiple: 1.35 });
  netMotif(s, [[1.0, 5.9, 0.08], [1.55, 6.2, 0.11], [2.15, 5.85, 0.08], [1.35, 5.55, 0.06]], [[0, 1], [1, 2], [1, 3]]);
  const rows = [
    ["精细权限", "四级角色 viewer / contributor / admin / owner；成员级控制「谁可以使用哪个智能体」。"],
    ["人工审批（HITL）", "高危 MCP 工具执行前弹出确认，可修改参数后放行，默认等待 10 分钟。"],
    ["凭据与传输安全", "OAuth 2.0 全流程；Token 以 AES-256-GCM 加密落库；网页抓取内置 SSRF 防护。"],
    ["全链路可观测", "Langfuse 追踪每轮思考与工具调用；空间级审计日志；支持全私有化部署。"],
  ];
  rows.forEach((r, i) => {
    const y = 1.72 + i * 1.22;
    s.addShape(P.shapes.ROUNDED_RECTANGLE, { x: 6.2, y: y + 0.03, w: 0.34, h: 0.34, rectRadius: 0.06, fill: { color: TINT }, line: { color: PRIMARY, width: 1 } });
    t(s, "✓", { x: 6.2, y: y + 0.03, w: 0.34, h: 0.34, fontSize: 13, bold: true, color: PRIMARY, align: "center", valign: "middle" });
    t(s, r[0], { x: 6.72, y, w: 6.0, h: 0.36, fontSize: 14.5, bold: true, color: TEXT });
    t(s, r[1], { x: 6.72, y: y + 0.4, w: 6.0, h: 0.62, fontSize: 11.5, color: MUTED, lineSpacingMultiple: 1.2 });
    if (i < 3) hline(s, 6.2, y + 1.06, 6.58);
  });
  s.addNotes("企业最大顾虑是安全:四级角色+成员级智能体访问+工具级审批+全链路审计,是 WeKnora 的企业级答案。");
}

/* ============ S12 平台关系图 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "05 · 关系图与 MCP 中台", "平台关系图：智能体如何连接一切");
  const bands = [
    { y: 1.5, label: "触达层", fill: TINT2, line: { color: LINEC, width: 1 }, labelC: PRIMARY, chipFill: WHITE, chipC: TEXT, rows: [["企业微信", "飞书", "钉钉", "Slack"], ["网页挂件", "开放 API", "MCP 客户端"]] },
    { y: 2.82, label: "智能体层", fill: PRIMARY, line: null, labelC: WHITE, chipFill: PANEL, chipC: WHITE, rows: [["ReAct 引擎：思考 → 分析 → 行动 → 观察"], ["自定义智能体", "长期记忆", "任务规划", "多模态"]] },
    { y: 4.14, label: "能力层", fill: TINT2, line: { color: LINEC, width: 1 }, labelC: PRIMARY, chipFill: WHITE, chipC: TEXT, rows: [["知识检索 RAG", "MCP 工具中台", "技能 + 沙箱", "联网搜索", "Wiki 工程"]] },
    { y: 5.46, label: "数据层", fill: TINT2, line: { color: LINEC, width: 1 }, labelC: PRIMARY, chipFill: WHITE, chipC: TEXT, rows: [["8 种向量库", "13 种文档格式", "对象存储", "PostgreSQL / Redis", "知识图谱"]] },
  ];
  const bx = M, bw = 8.55, bh = 1.08;
  bands.forEach((b, bi) => {
    rrect(s, bx, b.y, bw, bh, b.fill, b.line, 0.06);
    t(s, b.label, { x: bx + 0.22, y: b.y + bh / 2 - 0.2, w: 1.35, h: 0.4, fontSize: 13, bold: true, color: b.labelC });
    b.rows.forEach((row, ri) => {
      let x = bx + 1.72;
      row.forEach((c) => {
        const hot = c === "MCP 工具中台";
        const w = cjkW(c, 10.5) + 0.3;
        chip(s, x, b.y + (ri === 0 ? 0.14 : 0.58), c, {
          fill: hot ? ACCENT : b.chipFill, color: hot ? WHITE : b.chipC,
          bold: hot || bi === 1, pt: 10.5, h: 0.38, w,
          line: bi === 1 ? { color: EDGE, width: 1 } : undefined,
        });
        x += w + 0.12;
      });
    });
    if (bi < 3) {
      const gy = b.y + bh;
      s.addShape(P.shapes.LINE, { x: 4.8, y: gy + 0.01, w: 0, h: 0.22, line: { color: PRIMARY, width: 1.5, beginArrowType: "triangle", endArrowType: "triangle" } });
    }
  });
  /* 右侧治理卡 */
  rrect(s, 9.35, 1.5, 3.43, 5.04, INK, null, 0.08);
  t(s, "全程治理", { x: 9.68, y: 1.76, w: 2.6, h: 0.36, fontSize: 15.5, bold: true, color: WHITE });
  t(s, "贯穿每一层", { x: 9.68, y: 2.14, w: 2.6, h: 0.28, fontSize: 10.5, color: ACCENT, charSpacing: 1.5 });
  const gov = ["四级角色 RBAC", "成员级智能体访问控制", "工具级人工审批 HITL", "OAuth 2.0 · 凭据加密", "审计日志 · Langfuse 追踪", "私有化部署 · 数据不出域"];
  gov.forEach((g, i) => {
    dot(s, 9.68, 2.68 + i * 0.6 + 0.07);
    t(s, g, { x: 9.9, y: 2.68 + i * 0.6, w: 2.75, h: 0.36, fontSize: 11.5, color: MINT });
  });
  t(s, "关系图：渠道触达 → 智能体引擎 → 能力组件 → 数据底座，治理能力贯穿全链路。",
    { x: M, y: 6.72, w: 12.2, h: 0.32, fontSize: 11.5, color: MUTED });
  s.addNotes("自上而下四层;高亮的能力层「MCP 工具中台」是下一页的主角;右侧治理贯穿全链路。");
}

/* ============ S13 MCP 介绍 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "05 · 关系图与 MCP 中台", "MCP：智能体世界的「USB-C」");
  t(s, "MCP（Model Context Protocol）是为智能体连接工具与数据源而生的开放协议 —— 相当于智能体世界的「USB-C」。",
    { x: M, y: 1.52, w: 5.35, h: 1.5, fontSize: 17, bold: true, color: TEXT, lineSpacingMultiple: 1.35 });
  const lrows = [
    ["统一接入", "一个协议接入所有工具，不必为每个系统单独写适配"],
    ["双向流通", "既能调用别人的工具，也能把自己的能力开放出去"],
    ["生态复用", "Claude / Cursor 等主流客户端原生支持，工具一次开发处处可用"],
  ];
  lrows.forEach((r, i) => {
    const y = 3.3 + i * 1.05;
    dot(s, M, y + 0.07, 0.1, PRIMARY);
    t(s, r[0], { x: M + 0.28, y, w: 4.9, h: 0.34, fontSize: 14, bold: true, color: TEXT });
    t(s, r[1], { x: M + 0.28, y: y + 0.37, w: 5.0, h: 0.55, fontSize: 11.5, color: MUTED, lineSpacingMultiple: 1.2 });
  });
  /* 右侧双向图 */
  rrect(s, 6.75, 1.6, 5.5, 0.95, TINT2, { color: LINEC, width: 1 }, 0.07);
  t(s, "外部 MCP 工具服务", { x: 6.95, y: 1.74, w: 5.1, h: 0.32, fontSize: 13.5, bold: true, color: TEXT, align: "center" });
  t(s, "OA · CRM · 数据库 · 搜索引擎 · 内部 API", { x: 6.95, y: 2.1, w: 5.1, h: 0.28, fontSize: 10.5, color: MUTED, align: "center" });
  rrect(s, 8.35, 3.15, 2.3, 1.05, PRIMARY, null, 0.08);
  t(s, "WeKnora", { x: 8.35, y: 3.32, w: 2.3, h: 0.4, fontSize: 18, bold: true, color: WHITE, align: "center" });
  t(s, "智能体 + 知识底座", { x: 8.35, y: 3.74, w: 2.3, h: 0.28, fontSize: 10.5, color: MINT, align: "center" });
  rrect(s, 6.75, 5.35, 5.5, 0.95, TINT2, { color: LINEC, width: 1 }, 0.07);
  t(s, "Claude · Cursor · 企业自建智能体", { x: 6.95, y: 5.49, w: 5.1, h: 0.32, fontSize: 13.5, bold: true, color: TEXT, align: "center" });
  t(s, "任意 MCP 客户端", { x: 6.95, y: 5.85, w: 5.1, h: 0.28, fontSize: 10.5, color: MUTED, align: "center" });
  arrow(s, 9.5, 2.55, 0, 0.6, { end: true });
  t(s, "作为客户端 · 智能体调用外部工具", { x: 9.72, y: 2.68, w: 3.0, h: 0.35, fontSize: 10.5, bold: true, color: PRIMARY });
  arrow(s, 9.5, 4.2, 0, 1.15, { end: true });
  t(s, "作为服务端 · 开放 30+ 知识工具", { x: 9.72, y: 4.62, w: 3.0, h: 0.35, fontSize: 10.5, bold: true, color: PRIMARY });
  s.addNotes("双向是关键:既消费工具,也供给能力。WeKnora 的知识能力可以被任何 MCP 客户端复用。");
}

/* ============ S14 MCP 中台 ============ */
{
  const s = P.addSlide();
  s.background = { color: WHITE };
  header(s, "05 · 关系图与 MCP 中台", "MCP 中台：平台统一供工具，空间即开即用");
  /* 顶带 */
  rrect(s, M, 1.5, 12.28, 0.95, TINT2, { color: LINEC, width: 1 }, 0.07);
  t(s, "外部工具生态", { x: 0.85, y: 1.78, w: 1.6, h: 0.36, fontSize: 12.5, bold: true, color: PRIMARY });
  let tx = 2.7;
  ["企业 OA", "CRM · 工单", "数据库", "搜索引擎", "内部 API 网关"].forEach((c) => { tx += chip(s, tx, 1.77, c, { pt: 11, h: 0.42 }) + 0.14; });
  /* 中台 */
  rrect(s, M, 2.9, 12.28, 2.3, PRIMARY, null, 0.09);
  t(s, "WeKnora MCP 中台", { x: M, y: 3.08, w: 12.28, h: 0.4, fontSize: 17, bold: true, color: WHITE, align: "center" });
  t(s, "平台统一供给工具能力，所有空间即开即用", { x: M, y: 3.5, w: 12.28, h: 0.3, fontSize: 11, color: MINT, align: "center" });
  const minis1 = ["统一接入 · SSE / HTTP Streamable", "认证中心 · APIKey / Bearer / OAuth 2.0", "权限与审批 · 工具级 HITL"];
  minis1.forEach((m, i) => {
    const x = 0.85 + i * 3.98;
    rrect(s, x, 3.95, 3.7, 0.5, PANEL, null, 0.06);
    t(s, m, { x, y: 3.95, w: 3.7, h: 0.5, fontSize: 10.5, color: WHITE, align: "center", valign: "middle" });
  });
  const minis2 = ["凭据安全 · AES-256-GCM 加密落库", "连接管理 · 复用 / 空闲清理 / 失效自愈"];
  minis2.forEach((m, i) => {
    const x = 2.83 + i * 3.98;
    rrect(s, x, 4.55, 3.7, 0.5, PANEL, null, 0.06);
    t(s, m, { x, y: 4.55, w: 3.7, h: 0.5, fontSize: 10.5, color: WHITE, align: "center", valign: "middle" });
  });
  /* 底带 */
  rrect(s, M, 5.65, 12.28, 0.95, TINT2, { color: LINEC, width: 1 }, 0.07);
  t(s, "消费方", { x: 0.85, y: 5.93, w: 1.6, h: 0.36, fontSize: 12.5, bold: true, color: PRIMARY });
  tx = 2.7;
  ["空间 A 智能体", "空间 B 智能体", "共享空间", "Claude · Cursor", "企业自建 Agent"].forEach((c) => { tx += chip(s, tx, 5.92, c, { pt: 11, h: 0.42 }) + 0.14; });
  /* 双向箭头 */
  [3.3, 10.1].forEach((x) => {
    s.addShape(P.shapes.LINE, { x, y: 2.47, w: 0, h: 0.4, line: { color: PRIMARY, width: 1.5, beginArrowType: "triangle", endArrowType: "triangle" } });
    s.addShape(P.shapes.LINE, { x, y: 5.22, w: 0, h: 0.4, line: { color: PRIMARY, width: 1.5, beginArrowType: "triangle", endArrowType: "triangle" } });
  });
  t(s, "内置 MCP 服务 = 中台模式：平台一次配置 → 全空间可见，敏感信息自动隐藏，新工具全员即用。",
    { x: M, y: 6.82, w: 12.28, h: 0.35, fontSize: 12.5, bold: true, color: PRIMARY, align: "center" });
  s.addNotes("中台价值:工具接入一次、认证凭据平台托管、审批策略统一,业务空间零成本复用。");
}

/* ============ S15 总结 ============ */
{
  const s = P.addSlide();
  s.background = { color: INK };
  netMotif(s, [[11.4, 5.9, 0.09], [12.2, 6.3, 0.13], [12.75, 5.6, 0.09], [10.7, 6.5, 0.07]], [[0, 1], [1, 2], [1, 3]]);
  t(s, "智能体不是更会聊天的机器人，\n而是能调用你全部系统的「数字员工」。", { x: 0.8, y: 1.35, w: 11.8, h: 1.9, fontSize: 32, bold: true, color: WHITE, lineSpacingMultiple: 1.3 });
  hline(s, 0.8, 3.6, 11.7, "1E6B55");
  const stats = [["30+", "内置工具"], ["30+", "MCP 开放工具"], ["10", "个 IM 渠道"], ["20+", "大模型可插拔"]];
  stats.forEach((d, i) => {
    const x = 0.8 + i * 2.95;
    t(s, d[0], { x, y: 3.9, w: 2.7, h: 0.7, fontSize: 30, bold: true, color: ACCENT });
    t(s, d[1], { x, y: 4.62, w: 2.7, h: 0.32, fontSize: 12.5, color: MINT });
  });
  t(s, "开源（MIT）· 私有化部署 · Docker / K8s 一键上线 —— 从一个知识库 + 一个智能体开始。",
    { x: 0.8, y: 5.5, w: 11.7, h: 0.45, fontSize: 15, color: MINT });
  t(s, "WeKnora · weknora.weixin.qq.com · 2026.09", { x: 0.8, y: 6.9, w: 8, h: 0.3, fontSize: 10.5, color: "6E9488" });
  s.addNotes("收尾:回到数字员工定位;建议从一个小场景(如 IT Helpdesk)开始试点,两周内可验证价值。");
}

P.writeFile({ fileName: "/Users/xierenfeng/Projects/ai/weknora/agent-intro-ppt/WeKnora智能体介绍.pptx" })
  .then(() => console.log("done, slides =", pageNo + 2));

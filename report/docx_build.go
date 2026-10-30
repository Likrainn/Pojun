package report

// 报告导出的 docx 组装:任务总报告 + 单漏洞报告。
// 数据从 findings 表(ListFindingsForExport/GetFinding,含 report 正文与证据)来,
// 这里只负责排版;内置模板的两套版式见 BuildTaskDocx / BuildFindingDocx。

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chu0119/artex/db"
)

// TaskDocxInput 是总报告的输入。
type TaskDocxInput struct {
	TaskID      int64
	Title       string
	Goal        string
	GeneratedAt time.Time
	AssetCounts map[string]int
	Findings    []*db.DBFinding
	// AssetNames 资产 id → 可读名（域名/IP/URL）。nil/缺项回退 "id:N"（A8）。
	AssetNames map[int64]string
	// Retests finding id → 复测记录（最新在前）。报告带「复测记录」小节（A9）。
	Retests map[int64][]*db.FindingRetest
}

func sevRankKey(f *db.DBFinding) int {
	switch strings.ToLower(f.Severity) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}

// BuildTaskDocx 渲染任务总报告:
// 封面(标题/目标/时间) → 概览(资产分布 + 严重度分布 + 漏洞类别分布) →
// 漏洞明细(按类别折叠分组,类内按严重度降序;report 正文 Markdown 转纯文本
// 段落 + evidence 代码块)。同类漏洞×多主机的重复上报在类别层折叠呈现。
func BuildTaskDocx(in TaskDocxInput) *Docx {
	d := NewDocx()

	// 封面
	d.H1(in.Title)
	if in.Goal != "" {
		d.H3("任务目标")
		d.P(in.Goal)
	}
	d.P("")
	d.P(fmt.Sprintf("报告生成时间:%s", FormatTimeUTC(in.GeneratedAt)))
	d.P(fmt.Sprintf("漏洞总数:%d(严重 %d / 高危 %d / 中危 %d / 低危 %d)",
		len(in.Findings), countSev(in.Findings, "critical"), countSev(in.Findings, "high"),
		countSev(in.Findings, "medium"), countSev(in.Findings, "low")))
	d.PageBreak()

	// 概览
	d.H1("一、评估概览")
	if len(in.AssetCounts) > 0 {
		types := make([]string, 0, len(in.AssetCounts))
		for k := range in.AssetCounts {
			types = append(types, k)
		}
		sort.Strings(types)
		rows := make([][]string, 0, len(types))
		total := 0
		for _, ty := range types {
			rows = append(rows, []string{assetTypeCN(ty), fmt.Sprint(in.AssetCounts[ty])})
			total += in.AssetCounts[ty]
		}
		d.Table([]string{"资产类型", "数量"}, rows)
		d.P(fmt.Sprintf("资产合计:%d", total))
	}
	// 严重度分布表（行底色随严重度，A2）
	d.H2("漏洞严重度分布")
	rows := make([][]string, 0, 4)
	fills := make([]string, 0, 4)
	for _, sev := range []string{"critical", "high", "medium", "low"} {
		n := countSev(in.Findings, sev)
		if n > 0 {
			rows = append(rows, []string{SeverityCN(sev), fmt.Sprint(n)})
			fills = append(fills, SeverityHexSoft(sev))
		}
	}
	d.TableFills([]string{"严重度", "数量"}, rows, fills)

	// 漏洞类别分布(同类别多主机上报在这里折叠成一行——"永恒之蓝×16 主机"
	// 在总览就是一个数字,不用翻 16 页明细)
	if groups := groupByClass(in.Findings); len(groups) > 0 {
		d.H2("漏洞类别分布")
		rows := make([][]string, 0, len(groups))
		for _, g := range groups {
			rows = append(rows, []string{g.class, fmt.Sprint(len(g.items)), fmt.Sprint(len(g.assets))})
		}
		d.Table([]string{"漏洞类别", "条数", "涉及资产数"}, rows)
	}
	d.PageBreak()

	// 漏洞明细:按类别分组折叠(类内按严重度降序),类别按条数降序——
	// 同一漏洞在多台主机上是多条记录,但阅读时按类别看,每类一段。
	d.H1("二、漏洞明细")
	groups := groupByClass(in.Findings)
	for gi, g := range groups {
		d.H2(fmt.Sprintf("2.%d %s(共 %d 条)", gi+1, g.class, len(g.items)))
		for fi, f := range g.items {
			// 严重度着色（A2）：标题颜色随级别，一眼分诊
			if hex := SeverityHex(f.Severity); hex != "" {
				d.headingColored(fmt.Sprintf("2.%d.%d [%s] %s", gi+1, fi+1, SeverityCN(f.Severity), findingTitle(f)), 3, hex)
			} else {
				d.H3(fmt.Sprintf("2.%d.%d [%s] %s", gi+1, fi+1, SeverityCN(f.Severity), findingTitle(f)))
			}
			d.P(fmt.Sprintf("漏洞类别:%s", nz(f.VulnClass, "未分类")))
			if len(f.AssetIDs) > 0 {
				d.P(fmt.Sprintf("影响资产:%s", assetNames(f.AssetIDs, in.AssetNames)))
			}
			d.P(fmt.Sprintf("发现时间:%s · 发现者:%s", FormatTimeUTC(f.CreatedAt), nz(f.Worker, "-")))
			if strings.TrimSpace(f.Summary) != "" {
				d.H4("摘要")
				d.P(f.Summary)
			}
			if body := strings.TrimSpace(f.Report); body != "" {
				d.H4("详细报告")
				writeMarkdownish(d, body)
			}
			if ev := strings.TrimSpace(f.Evidence); ev != "" {
				d.H4("证据 / PoC")
				d.Code(ev)
			}
			writeTrafficEvidenceDocx(d, f)
			writeRetestDocx(d, f, in.Retests)
			d.P("")
		}
	}
	return d
}

// assetNames 资产 id 列表 → 可读名（A8）。缺映射的回退 "id:N"。
func assetNames(ids []int64, names map[int64]string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if n, ok := names[id]; ok && strings.TrimSpace(n) != "" {
			parts = append(parts, n)
			continue
		}
		parts = append(parts, fmt.Sprintf("id:%d", id))
	}
	return strings.Join(parts, ", ")
}

// retestVerdictCN 复测结论中文（A9）。
func retestVerdictCN(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "reproduced":
		return "已复现"
	case "fixed":
		return "已修复"
	case "inconclusive":
		return "无法定论"
	default:
		return nz(v, "-")
	}
}

// writeRetestDocx 复测记录小节（A9）：最新一次结论在前，历史条数附注。
func writeRetestDocx(d *Docx, f *db.DBFinding, retests map[int64][]*db.FindingRetest) {
	rs := retests[f.ID]
	if len(rs) == 0 {
		return
	}
	latest := rs[0]
	d.H4("复测记录")
	line := fmt.Sprintf("最近复测:%s · %s", retestVerdictCN(latest.Verdict), FormatTimeUTC(latest.CreatedAt))
	if s := strings.TrimSpace(latest.Summary); s != "" {
		line += " · " + s
	}
	d.P(line)
	if len(rs) > 1 {
		d.P(fmt.Sprintf("历史复测共 %d 次（可在平台「发现 → 复测」查看全过程）。", len(rs)))
	}
}

// trafficRoleCN 流量证据角色中文（A10）。
func trafficRoleCN(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "baseline":
		return "正常对照"
	case "proof":
		return "漏洞证明"
	case "verification":
		return "补充验证"
	case "supporting":
		return "辅助证据"
	default:
		return nz(role, "-")
	}
}

// writeTrafficEvidenceDocx 流量证据小节（A10）：md 导出（report/findings.go）已有
// 此逻辑，这里在 Word 侧同构呈现——绑定元信息 + 请求/响应头代码块。报文正文
// 体积大且含二进制形态，交付件保留头部与快照元信息（正文走 md 导出的附件文件）。
func writeTrafficEvidenceDocx(d *Docx, f *db.DBFinding) {
	if len(f.TrafficBindings) == 0 {
		return
	}
	stale := f.Report != "" && f.EvidenceVersion != f.ReportEvidenceVersion
	d.H4("关联流量证据")
	d.P(fmt.Sprintf("证据版本:%d · 绑定 %d 条", f.EvidenceVersion, len(f.TrafficBindings)))
	if stale {
		d.P("⚠ 证据已变更，详细报告待更新。")
	}
	for i, b := range f.TrafficBindings {
		d.P(fmt.Sprintf("%d. 证据 #%d · %s — %s %s · 状态码 %d",
			i+1, b.ID, trafficRoleCN(b.Role), nz(b.Snapshot.Method, "GET"), b.Snapshot.URL, b.Snapshot.Status))
		if n := strings.TrimSpace(b.Note); n != "" {
			d.PRich("   说明:" + n)
		}
		if h := strings.TrimSpace(b.Snapshot.ReqHead); h != "" {
			d.P("请求头:")
			d.Code(h)
		}
		if h := strings.TrimSpace(b.Snapshot.RespHead); h != "" {
			d.P("响应头:")
			d.Code(h)
		}
	}
}

// classGroup 同一漏洞类别的发现聚合(含去重资产集合)。
// 归一用 db.NormalizeVulnClass:写入层已归一新记录,历史变体(SQLi vs 注入)
// 在读取侧同样折叠,总报告不因旧数据开天窗。
type classGroup struct {
	class  string
	items  []*db.DBFinding
	assets map[int64]struct{}
}

func groupByClass(fs []*db.DBFinding) []classGroup {
	idx := map[string]*classGroup{}
	var order []string
	for _, f := range fs {
		c := db.NormalizeVulnClass(f.VulnClass)
		if c == "" {
			c = "未分类"
		}
		g, ok := idx[c]
		if !ok {
			g = &classGroup{class: c, assets: map[int64]struct{}{}}
			idx[c] = g
			order = append(order, c)
		}
		g.items = append(g.items, f)
		for _, a := range f.AssetIDs {
			g.assets[a] = struct{}{}
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		gi, gj := idx[order[i]], idx[order[j]]
		// 条数降序,同条数按类别名字典序保证稳定
		if len(gi.items) != len(gj.items) {
			return len(gi.items) > len(gj.items)
		}
		return gi.class < gj.class
	})
	out := make([]classGroup, 0, len(order))
	for _, c := range order {
		g := idx[c]
		sort.SliceStable(g.items, func(i, j int) bool { return sevRankKey(g.items[i]) < sevRankKey(g.items[j]) })
		out = append(out, *g)
	}
	return out
}

// BuildFindingDocx 渲染单漏洞报告(复现步骤/证据/修复建议的自包含文档)。
func BuildFindingDocx(f *db.DBFinding, generatedAt time.Time, assetNameMap map[int64]string, retests []*db.FindingRetest) *Docx {
	d := NewDocx()
	if hex := SeverityHex(f.Severity); hex != "" {
		d.headingColored(fmt.Sprintf("[%s] %s", SeverityCN(f.Severity), findingTitle(f)), 1, hex)
	} else {
		d.H1(fmt.Sprintf("[%s] %s", SeverityCN(f.Severity), findingTitle(f)))
	}
	d.P(fmt.Sprintf("漏洞类别:%s · 严重度:%s", nz(f.VulnClass, "未分类"), SeverityCN(f.Severity)))
	if len(f.AssetIDs) > 0 {
		d.P(fmt.Sprintf("影响资产:%s", assetNames(f.AssetIDs, assetNameMap)))
	}
	d.P(fmt.Sprintf("发现时间:%s · 报告生成:%s", FormatTimeUTC(f.CreatedAt), FormatTimeUTC(generatedAt)))
	if s := strings.TrimSpace(f.Summary); s != "" {
		d.H2("一、漏洞概述")
		d.P(s)
	}
	if body := strings.TrimSpace(f.Report); body != "" {
		d.H2("二、详细报告")
		writeMarkdownish(d, body)
	}
	if ev := strings.TrimSpace(f.Evidence); ev != "" {
		d.H2("三、证据 / PoC")
		d.Code(ev)
	}
	// 四、流量证据 / 五、复测记录 —— 与总报告同源（A9/A10）
	writeTrafficEvidenceDocx(d, f)
	if len(retests) > 0 {
		d.H2("复测记录")
		latest := retests[0]
		line := fmt.Sprintf("最近复测:%s · %s", retestVerdictCN(latest.Verdict), FormatTimeUTC(latest.CreatedAt))
		if s := strings.TrimSpace(latest.Summary); s != "" {
			line += " · " + s
		}
		d.P(line)
		if len(retests) > 1 {
			d.P(fmt.Sprintf("历史复测共 %d 次。", len(retests)))
		}
	}
	return d
}

// writeMarkdownish 把 finding.report 的 Markdown 排进 Word（A5 富渲染）：
// - '# '/'## '/'### ' → H3/H4/H4（漏洞标题已是 H3，正文小节不再压平）
// - '- '/'* ' → 项目符号（numbering.xml 修复后真实渲染）
// - '```' 围栏 → 代码块
// - '| |' 表格块 → 真 OOXML 表格（第二行分隔行自动剔除）
// - '**加粗**' → 富文本 run（段落与列表项都支持）
// 其余按段落。不追求 Markdown 完整性——报告正文是模型产物，覆盖常用子集即可。
func writeMarkdownish(d *Docx, md string) {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	inCode := false
	// 表格块收集：连续的 | 开头行
	var tableBuf []string
	flushTable := func() {
		if len(tableBuf) == 0 {
			return
		}
		rows := make([][]string, 0, len(tableBuf))
		for _, ln := range tableBuf {
			cells := splitMarkdownRow(ln)
			if isTableSeparator(cells) {
				continue // |---|---| 分隔行
			}
			rows = append(rows, cells)
		}
		if len(rows) > 0 {
			header := rows[0]
			body := rows[1:]
			if len(body) == 0 {
				// 只有表头一行的退化表格按普通段落打出来
				d.P(strings.Join(header, " | "))
			} else {
				d.Table(header, body)
			}
		}
		tableBuf = tableBuf[:0]
	}
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") {
			flushTable()
			inCode = !inCode
			continue
		}
		if inCode {
			d.buf.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="Code"/><w:shd w:val="clear" w:fill="F2F2F2"/></w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, esc(ln)))
			continue
		}
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			tableBuf = append(tableBuf, trimmed)
			continue
		}
		flushTable()
		switch {
		case strings.HasPrefix(trimmed, "### "):
			d.H4(strings.TrimPrefix(trimmed, "### "))
		case strings.HasPrefix(trimmed, "## "):
			d.H4(strings.TrimPrefix(trimmed, "## "))
		case strings.HasPrefix(trimmed, "# "):
			d.H3(strings.TrimPrefix(trimmed, "# "))
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			d.buf.WriteString(`<w:p><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr>`)
			d.buf.WriteString(runsXML(parseBold(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* "))))
			d.buf.WriteString(`</w:p>`)
		case trimmed == "":
			// 段间空行,段落间距已由样式给,跳过
		default:
			d.PRich(trimmed)
		}
	}
	flushTable()
}

// splitMarkdownRow 拆一行 | a | b | → [a, b]。
func splitMarkdownRow(ln string) []string {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(ln), "|"), "|")
	parts := strings.Split(trimmed, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// isTableSeparator 识别 |---|:---:| 分隔行。
func isTableSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		t := strings.TrimSpace(c)
		if t == "" {
			return false
		}
		for _, r := range t {
			if r != '-' && r != ':' && r != ' ' {
				return false
			}
		}
	}
	return true
}

func countSev(fs []*db.DBFinding, sev string) int {
	n := 0
	for _, f := range fs {
		if strings.EqualFold(f.Severity, sev) {
			n++
		}
	}
	return n
}

func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, v := range ids {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ", ")
}

// assetTypeCN 资产类型中文(概览表用)。
func assetTypeCN(ty string) string {
	switch ty {
	case "root_domain":
		return "根域名"
	case "subdomain":
		return "子域名"
	case "ip":
		return "IP"
	case "service":
		return "服务"
	case "endpoint":
		return "端点"
	case "app":
		return "应用"
	default:
		return ty
	}
}

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
	// 严重度分布表
	d.H2("漏洞严重度分布")
	rows := make([][]string, 0, 4)
	for _, sev := range []string{"critical", "high", "medium", "low"} {
		n := countSev(in.Findings, sev)
		if n > 0 {
			rows = append(rows, []string{SeverityCN(sev), fmt.Sprint(n)})
		}
	}
	d.Table([]string{"严重度", "数量"}, rows)

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
			d.H3(fmt.Sprintf("2.%d.%d [%s] %s", gi+1, fi+1, SeverityCN(f.Severity), findingTitle(f)))
			d.P(fmt.Sprintf("漏洞类别:%s", nz(f.VulnClass, "未分类")))
			if len(f.AssetIDs) > 0 {
				d.P(fmt.Sprintf("影响资产 id:%s", joinIDs(f.AssetIDs)))
			}
			d.P(fmt.Sprintf("发现时间:%s · 发现者:%s", FormatTimeUTC(f.CreatedAt), nz(f.Worker, "-")))
			if strings.TrimSpace(f.Summary) != "" {
				d.H3("摘要")
				d.P(f.Summary)
			}
			if body := strings.TrimSpace(f.Report); body != "" {
				d.H3("详细报告")
				writeMarkdownish(d, body)
			}
			if ev := strings.TrimSpace(f.Evidence); ev != "" {
				d.H3("证据 / PoC")
				d.Code(ev)
			}
			d.P("")
		}
	}
	return d
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
func BuildFindingDocx(f *db.DBFinding, generatedAt time.Time) *Docx {
	d := NewDocx()
	d.H1(fmt.Sprintf("[%s] %s", SeverityCN(f.Severity), findingTitle(f)))
	d.P(fmt.Sprintf("漏洞类别:%s · 严重度:%s", nz(f.VulnClass, "未分类"), SeverityCN(f.Severity)))
	if len(f.AssetIDs) > 0 {
		d.P(fmt.Sprintf("影响资产 id:%s", joinIDs(f.AssetIDs)))
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
	return d
}

// writeMarkdownish 把 finding.report 的 Markdown 以纯文本段落近似排版:
// - '# '/'## '/'### ' 开头 → 小标题
// - '- '/'* ' 开头 → 列表项
// - '```' 围栏 → 代码块(成对处理)
// 其余按段落。不追求 Markdown 完整性——报告正文是模型产物,近似排版已够读。
func writeMarkdownish(d *Docx, md string) {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	inCode := false
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			d.buf.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="Code"/><w:shd w:val="clear" w:fill="F2F2F2"/></w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, esc(ln)))
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "### "):
			d.H3(strings.TrimPrefix(trimmed, "### "))
		case strings.HasPrefix(trimmed, "## "):
			d.H3(strings.TrimPrefix(trimmed, "## "))
		case strings.HasPrefix(trimmed, "# "):
			d.H3(strings.TrimPrefix(trimmed, "# "))
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			d.Bullet(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* "))
		case trimmed == "":
			// 段间空行,段落间距已由样式给,跳过
		default:
			d.P(trimmed)
		}
	}
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

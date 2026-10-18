package agent

// 漏洞情报工具（vulnintel）：worker/planner/mainagent 在识别出目标组件后，
// 用 search_vulns 查公开已知漏洞（CISA KEV 优先排序），get_vuln 取单条详情
// （完整描述 + CPE criteria 版本区间 + 参考链接）决定利用路径。
// 情报库全库共享且对 agent 只读，因此走包级注入（同 ToolResolve 钩子的模式），
// server 启动时 Set 一次；nil = 工具可用但提示未启用（与 asset 工具的 guard 一致）。

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"time"

	actool "github.com/Autumn-27/norma/tool"

	"github.com/chu0119/artex/vulnintel"
)

// vulnintelStore is process-wide (set once at server startup); safe for
// concurrent ToolSets because the store is read-only from the agent's side.
var vulnintelStore *vulnintel.Store

// SetVulnIntelStore wires the shared intel store; nil disables the tools.
func SetVulnIntelStore(s *vulnintel.Store) { vulnintelStore = s }

// nucleiTemplatesDir is the local nuclei-templates checkout (empty = scan tool
// unavailable). Set alongside the store at startup.
var nucleiTemplatesDir string

// SetNucleiTemplatesDir wires the template tree path; empty disables scan tool.
func SetNucleiTemplatesDir(dir string) { nucleiTemplatesDir = dir }

// searchVulns exposes product-keyed CVE retrieval. Version matching against
// NVD criteria is intentionally returned as raw material (criteria text + a
// coarse "version seen" flag) — the LLM judges applicability, structured CPE
// range math is a later batch.
func (t *ToolSet) searchVulns() actool.CoreTool {
	return readTool(
		"search_vulns",
		"按组件名检索【公开已知漏洞】库（NVD + CISA KEV 在野利用清单）。识别出目标组件"+
			"（Web 中间件/框架/服务/固件等）后先查这里：有 KEV 标记=已在野利用、优先考虑；"+
			"结果里的 cvss_score 与 severity 用于排优先级。version 参数可给已知版本号做粗筛"+
			"（version_seen_in_criteria=精确命中过 CPE 版本段）；版本是否真正落在影响区间，"+
			"用 get_vuln 看 cpe_criteria 的版本区间语法（versionEndExcluding 等）自行判断。"+
			"返回条数有限，命中多时提高 limit 或加 vendor 收窄。",
		obj(map[string]any{
			"product": str("组件名，如 httpd / nginx / thinkphp / tomcat / weblogic；支持 % 通配"),
			"vendor":  str("厂商名过滤(可选)，如 apache / oracle，用于区分同名组件"),
			"version": str("目标版本号(可选，如 2.4.49)：命中过 CPE 版本段的结果会标记 version_seen_in_criteria"),
			"limit":   obj(map[string]any{"type": "integer", "description": "返回条数上限(默认 20，最大 50)"}),
		}, "product"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if vulnintelStore == nil {
				return actool.Errorf("search_vulns 未启用: 漏洞情报库未初始化"), nil
			}
			var a struct {
				Product string `json:"product"`
				Vendor  string `json:"vendor"`
				Version string `json:"version"`
				Limit   int    `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			if strings.TrimSpace(a.Product) == "" {
				return actool.Errorf("product 不能为空"), nil
			}
			hits, err := vulnintelStore.SearchVulns(context.Background(), a.Vendor, a.Product, a.Version, a.Limit)
			if err != nil {
				return actool.Errorf("检索漏洞失败: " + err.Error()), nil
			}
			if len(hits) == 0 {
				return jsonResult(map[string]any{
					"count": 0,
					"note":  "该组件名在情报库无命中（可能拼写与 CPE 命名不符，试换常见缩写如 httpd/IIS；或该组件近期无已知漏洞）",
				})
			}
			kev := 0
			for _, h := range hits {
				if h.KEV {
					kev++
				}
			}
			return jsonResult(map[string]any{
				"count":           len(hits),
				"kev_count":       kev,
				"vulnerabilities": hits,
				"note":            "kev=true 表示 CISA 已确认在野利用，价值最高；下一步用 get_vuln 看单条详情（影响版本区间/参考链接）",
			})
		},
	)
}

// getVuln exposes the full record for one CVE.
func (t *ToolSet) getVuln() actool.CoreTool {
	return readTool(
		"get_vuln",
		"取单条已知漏洞的完整详情：英文原始描述、CVSS 评分与向量、是否 KEV、"+
			"cpe_criteria（NVD 的影响范围原文，cpe:2.3:a:厂商:组件:版本:... 字符串，"+
			"配套 versionEndExcluding / versionStartIncluding 等区间字段表达影响范围）、"+
			"references（利用/公告/补丁链接）。用它判断目标版本是否命中、以及去哪找现成利用。",
		obj(map[string]any{
			"cve_id": str("CVE 编号，如 CVE-2021-41773"),
		}, "cve_id"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if vulnintelStore == nil {
				return actool.Errorf("get_vuln 未启用: 漏洞情报库未初始化"), nil
			}
			var a struct {
				CVEID string `json:"cve_id"`
			}
			_ = json.Unmarshal(in, &a)
			if strings.TrimSpace(a.CVEID) == "" {
				return actool.Errorf("cve_id 不能为空"), nil
			}
			d, err := vulnintelStore.GetVuln(context.Background(), a.CVEID)
			if err != nil {
				return actool.Errorf("查询漏洞失败: " + err.Error()), nil
			}
			if d == nil {
				return actool.Errorf(a.CVEID + " 不在情报库中（库只收录公开同步的数据源）"), nil
			}
			return jsonResult(d)
		},
	)
}

// getPoc exposes aggregated PoC/EXP entries for one CVE.
func (t *ToolSet) getPoc() actool.CoreTool {	return readTool(
		"get_poc",
		"取单条已知漏洞的【现成 PoC/利用】聚合：GitHub 上引用该 CVE 的公开仓库，"+
			"按星数排序。先 search_vulns 确认漏洞存在，再 get_poc 找现成弹药；返回的是"+
			"仓库地址与描述，不替你执行——用 Bash clone/分析仓库内容、按其说明构造利用。"+
			"没有命中只代表公开仓库没有现成代码，不代表漏洞不可利用。",
		obj(map[string]any{
			"cve_id": str("CVE 编号，如 CVE-2021-41773"),
		}, "cve_id"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if vulnintelStore == nil {
				return actool.Errorf("get_poc 未启用: 漏洞情报库未初始化"), nil
			}
			var a struct {
				CVEID string `json:"cve_id"`
			}
			_ = json.Unmarshal(in, &a)
			if strings.TrimSpace(a.CVEID) == "" {
				return actool.Errorf("cve_id 不能为空"), nil
			}
			hits, err := vulnintelStore.GetPoCs(context.Background(), a.CVEID)
			if err != nil {
				return actool.Errorf("查询 PoC 失败: " + err.Error()), nil
			}
			return jsonResult(map[string]any{
				"count": len(hits),
				"pocs":  hits,
			})
		},
	)
}


// scanWithNuclei assembles a nuclei command for the worker's Bash tool. The
// tool deliberately does NOT execute nuclei itself: Bash runs it through the
// recording proxy + approval gates like every other command, keeping audit
// semantics uniform. What this tool adds is template selection (CVE-keyed)
// and a correct, version-pinned command line.
func (t *ToolSet) scanWithNuclei() actool.CoreTool {
	return readTool(
		"scan_with_nuclei",
		"组装一条【nuclei 模板扫描】命令交给 Bash 执行(本工具只组命令不执行)。"+
			"适合已确认目标可达、且 search_vulns 找到对应 CVE 后做批量模板验证。"+
			"give cve_id 时只跑命中该 CVE 的模板(推荐,快且聚焦);不给则跑全模板树"+
			"(慢,慎用,建议配合 severity 收窄)。模板库随情报同步自动更新。"+
			"命令输出为 JSONL 结果,每行一个发现;没有输出=没有模板命中。",
		obj(map[string]any{
			"target":   str("扫描目标 URL,如 https://example.com"),
			"cve_id":   str("CVE 编号(可选,推荐):只跑命中该 CVE 的模板,如 CVE-2021-41773"),
			"severity": str("严重度过滤(可选):critical,high,medium,low 逗号组合"),
		}, "target"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if nucleiTemplatesDir == "" {
				return actool.Errorf("scan_with_nuclei 未启用: 模板库路径未配置"), nil
			}
			if !vulnintel.NucleiInstalled() {
				return actool.Errorf("nuclei 未安装(当前运行环境不含该二进制;Docker 镜像已内置)"), nil
			}
			var a struct {
				Target   string `json:"target"`
				CVEID    string `json:"cve_id"`
				Severity string `json:"severity"`
			}
			_ = json.Unmarshal(in, &a)
			cmd, cancel, err := vulnintel.BuildNucleiCmd(context.Background(), nucleiTemplatesDir, vulnintel.ScanArgs{
				Target:       a.Target,
				CVEID:        a.CVEID,
				Severity:     a.Severity,
				TimeoutTotal: 10 * time.Minute,
			})
			if err != nil {
				return actool.Errorf("组装扫描命令失败: " + err.Error()), nil
			}
			defer cancel()
			return jsonResult(map[string]any{
				"run_with_bash": strings.Join(cmd.Args, " "),
				"note": "用 Bash 工具执行上面这条命令(默认 10 分钟超时,超时会截断输出);" +
					"输出为 JSONL,每行一个模板命中;空输出=无命中。发现真实漏洞后走 report_finding 走正常上报。",
			})
		},
	)
}

// netContext 输出当前运行环境的网络上下文(网卡/路由/用户配置的回连信息),
// 供 reverse shell / bind shell / MSF 的 LHOST/LPORT 决策。
// 刻意不 exec ip 命令(镜像里 iproute2 不保证有),用 Go 原生枚举。
func (t *ToolSet) netContext() actool.CoreTool {
	return readTool(
		"net_context",
		"获取本机网络上下文:全部网卡 IP(容器内视角)、以及平台配置的【回连可达地址】。"+
			"在容器里运行时,本机 IP 是容器内网地址,目标大概率连不进来——起 reverse shell / "+
			"MSF handler 前【必须】先看本工具:用配置的公网 IP/宿主映射地址做 LHOST,端口用"+
			"已映射的端口段;bind shell 或目标同网段时才可用本机内网 IP。",
		obj(map[string]any{}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			type iface struct {
				Name string   `json:"name"`
				IPs  []string `json:"ips"`
			}
			var ifaces []iface
			addrs, err := net.Interfaces()
			if err == nil {
				for _, a := range addrs {
					if a.Flags&net.FlagUp == 0 {
						continue
					}
					addrs2, err := a.Addrs()
					if err != nil || len(addrs2) == 0 {
						continue
					}
					it := iface{Name: a.Name, IPs: []string{}}
					for _, ad := range addrs2 {
						it.IPs = append(it.IPs, ad.String())
					}
					ifaces = append(ifaces, it)
				}
			}
			var hint, portMap string
			if vulnintelStore != nil {
				hint = vulnintelStore.EgressHint(ctx)
				portMap = vulnintelStore.EgressPortMap(ctx)
			}
			return jsonResult(map[string]any{
				"interfaces":          ifaces,
				"egress_hint":         hint,
				"egress_port_map":     portMap,
				"note": "interfaces 是【容器内视角】,对外不可达;egress_hint 是平台配置的对外可达地址与映射说明。"+
					"reverse shell 的 LHOST 用 hint 里的可达地址,监听端口必须在 port_map 声明过的映射范围内。" +
					"hint 为空说明平台未配置——请用 add_hint 请求人工提供,不要拿容器 IP 盲目回连。",
			})
		},
	)
}

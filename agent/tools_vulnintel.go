package agent

// 漏洞情报工具（vulnintel）：worker/planner/mainagent 在识别出目标组件后，
// 用 search_vulns 查公开已知漏洞（CISA KEV 优先排序），get_vuln 取单条详情
// （完整描述 + CPE criteria 版本区间 + 参考链接）决定利用路径。
// 情报库全库共享且对 agent 只读，因此走包级注入（同 ToolResolve 钩子的模式），
// server 启动时 Set 一次；nil = 工具可用但提示未启用（与 asset 工具的 guard 一致）。

import (
	"context"
	"encoding/json"
	"strings"

	actool "github.com/Autumn-27/norma/tool"

	"github.com/chu0119/artex/vulnintel"
)

// vulnintelStore is process-wide (set once at server startup); safe for
// concurrent ToolSets because the store is read-only from the agent's side.
var vulnintelStore *vulnintel.Store

// SetVulnIntelStore wires the shared intel store; nil disables the tools.
func SetVulnIntelStore(s *vulnintel.Store) { vulnintelStore = s }

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
func (t *ToolSet) getPoc() actool.CoreTool {
	return readTool(
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


// Package hosttools:宿主工具体检 + 安装配方。
//
// 二进制部署形态下,ARTEX 直接跑在操作系统里——宿主装了什么渗透工具,
// worker 就能通过 Bash 工具直接调用什么。这里用 exec.LookPath 探测一份
// 常用工具清单(可用/缺失+安装提示),注入 graph_overview 让模型派意图时
// 知道家底,并通过 /api/tools/inventory 暴露给前端展示。
//
// 同一份清单同时驱动「一键安装」:每条工具带一个 Kind + 配方(apt 包名 /
// GitHub Release 资产模板 / pip 包 / 仅手工),由特权助手 artex-hostd 按
// 配方执行。**体检清单和安装器共用这一个 catalog**,不存在"页面上列了、
// 安装器却不会装"的漂移。
//
// LookPath 只扫 PATH,单次微秒级,无需缓存。
package hosttools

import (
	"os/exec"
	"runtime"
	"strings"
)

// Kind 安装方式。manual = 官方只提供交互式安装器,不做一键。
type Kind string

const (
	KindApt    Kind = "apt"    // 系统包管理器(apt/dnf)
	KindGitHub Kind = "github" // GitHub Release 单文件
	KindPip    Kind = "pip"    // pip / pipx
	KindManual Kind = "manual" // 不提供一键安装
)

// Tool 一条体检结果 + 安装配方。
type Tool struct {
	ID   string `json:"id"`   // 稳定标识(与 Bin 相同)
	Name string `json:"name"` // 工具名(展示用)
	Bin  string `json:"bin"`  // PATH 里探测的二进制名

	Available   bool   `json:"available"`
	Installable bool   `json:"installable"`      // 能否一键安装
	Purpose     string `json:"purpose"`          // 干什么用(给模型看)
	Install     string `json:"install,omitempty"` // 缺失时展示的安装提示
	Kind        Kind   `json:"kind"`

	// kind=apt: 各包管理器下的包名
	Apt string `json:"-"`
	Dnf string `json:"-"`

	// kind=github: owner/repo + 资产名模板({ver} 不带 v,{arch} = amd64|arm64)
	// + 压缩包内二进制名(空 = 与 Bin 同名)
	Repo      string `json:"-"`
	AssetTmpl string `json:"-"`
	BinInZip  string `json:"-"`

	// kind=pip
	Pip string `json:"-"`
}

// catalog 常用渗透工具目录。安装方式优先选「自包含」的:能用 GitHub Release
// 单文件就不依赖 go install(宿主未必有 Go 工具链)。
var catalog = []Tool{
	{Name: "nmap", Purpose: "端口扫描/服务指纹", Kind: KindApt, Apt: "nmap", Dnf: "nmap"},
	{Name: "masscan", Purpose: "大规模端口快扫", Kind: KindApt, Apt: "masscan", Dnf: "masscan"},
	{Name: "sqlmap", Purpose: "SQL 注入检测与利用", Kind: KindApt, Apt: "sqlmap", Dnf: "sqlmap"},
	{Name: "ffuf", Purpose: "目录/参数模糊枚举", Kind: KindApt, Apt: "ffuf", Dnf: "ffuf"},
	{Name: "gobuster", Purpose: "目录/DNS/vhost 爆破", Kind: KindApt, Apt: "gobuster", Dnf: "gobuster"},
	{Name: "nikto", Purpose: "Web 服务器常见问题扫描", Kind: KindApt, Apt: "nikto", Dnf: "nikto"},
	{Name: "whatweb", Purpose: "Web 指纹识别", Kind: KindApt, Apt: "whatweb", Dnf: "whatweb"},
	{Name: "hydra", Purpose: "在线口令爆破(ssh/登录表单等)", Kind: KindApt, Apt: "hydra", Dnf: "hydra"},
	{Name: "john", Purpose: "离线口令破解", Kind: KindApt, Apt: "john", Dnf: "john"},
	{Name: "hashcat", Purpose: "GPU 口令破解", Kind: KindApt, Apt: "hashcat", Dnf: "hashcat"},
	{Name: "curl", Purpose: "HTTP 请求与调试", Kind: KindApt, Apt: "curl", Dnf: "curl"},
	{Name: "openssl", Purpose: "TLS 证书检查/加解密", Kind: KindApt, Apt: "openssl", Dnf: "openssl"},
	{Name: "python3", Purpose: "脚本执行/自定义 PoC", Kind: KindApt, Apt: "python3", Dnf: "python3"},
	{Name: "nc", Purpose: "TCP/UDP 连接与监听(反连交互)", Kind: KindApt, Apt: "netcat-openbsd", Dnf: "nmap-ncat"},
	{Name: "dig", Purpose: "DNS 查询", Kind: KindApt, Apt: "dnsutils", Dnf: "bind-utils"},
	{Name: "ssh", Purpose: "登录验证/隧道", Kind: KindApt, Apt: "openssh-client", Dnf: "openssh-clients"},
	{Name: "testssl.sh", Bin: "testssl", Purpose: "TLS 配置弱项扫描", Kind: KindApt, Apt: "testssl.sh", Dnf: "testssl"},
	// nodejs 连 npx 一起装：MCP 里用 `npx @playwright/mcp` 启动 browser 服务器，
	// 机器上没有 node 时那个 MCP 永远连不上（实测每次 worker 启动都报
	// "npx: executable file not found in $PATH"）。
	{Name: "nodejs", Bin: "node", Purpose: "Node 运行时(npx 随装,供 MCP 服务器用)", Kind: KindApt, Apt: "nodejs", Dnf: "nodejs"},

	{Name: "nuclei", Purpose: "基于模板的漏洞扫描(漏洞情报联动)", Kind: KindGitHub,
		Repo: "projectdiscovery/nuclei", AssetTmpl: "nuclei_{ver}_linux_{arch}.zip"},
	{Name: "httpx", Purpose: "Web 存活/指纹批量探测(projectdiscovery 版)", Kind: KindGitHub,
		Repo: "projectdiscovery/httpx", AssetTmpl: "httpx_{ver}_linux_{arch}.zip"},
	{Name: "subfinder", Purpose: "子域名枚举", Kind: KindGitHub,
		Repo: "projectdiscovery/subfinder", AssetTmpl: "subfinder_{ver}_linux_{arch}.zip"},
	// ProjectDiscovery 系另外三个：naabu(端口扫)、dnsx(DNS 解析/爆破)、
	// katana(爬虫)、tlsx(TLS 证书探测)。资产命名与上面三个同构
	// （{tool}_{ver}_linux_{arch}.zip），所以一个 AssetTmpl 模板就够。
	{Name: "naabu", Purpose: "快速端口扫描(与 httpx 联动筛存活)", Kind: KindGitHub,
		Repo: "projectdiscovery/naabu", AssetTmpl: "naabu_{ver}_linux_{arch}.zip"},
	{Name: "dnsx", Purpose: "DNS 解析/爆破/记录提取", Kind: KindGitHub,
		Repo: "projectdiscovery/dnsx", AssetTmpl: "dnsx_{ver}_linux_{arch}.zip"},
	{Name: "katana", Purpose: "Web 爬虫(爬深层路径与参数)", Kind: KindGitHub,
		Repo: "projectdiscovery/katana", AssetTmpl: "katana_{ver}_linux_{arch}.zip"},
	{Name: "tlsx", Purpose: "TLS 证书信息提取(证书/ SAN / 过期)", Kind: KindGitHub,
		Repo: "projectdiscovery/tlsx", AssetTmpl: "tlsx_{ver}_linux_{arch}.zip"},

	{Name: "wafw00f", Purpose: "WAF 识别", Kind: KindPip, Pip: "wafw00f"},
	// impacket 是 SMB/NTLM/票据的 Python 库(提供 msmfmap、ntlmrelay、secretsdump
	// 等脚本)。apt 源里的包名是 python3-impacket,Debian/Ubuntu 都有。
	{Name: "impacket", Purpose: "SMB/NTLM 协议工具集(secretsdump/ntlmrelay 等)", Kind: KindApt,
		Apt: "python3-impacket", Dnf: "python3-impacket"},
	// SecLists 是最大的通用字典集(目录爆破/参数 fuzz 的语料)。apt 源里没有
	// 稳定包名,只能从 GitHub 克隆——但它是目录而非单文件,GitHub 配方装不了,
	// 所以走 KindManual 并在 install.sh 里单独克隆。
	{Name: "seclists", Purpose: "通用字典集(目录/参数/用户名爆破语料)", Kind: KindManual,
		Install: "git clone --depth 1 https://github.com/danielmiessler/SecLists.git /opt/SecLists"},

	{Name: "msfconsole", Purpose: "Metasploit 框架(exploit/encoder)", Kind: KindManual,
		Install: "官方安装包(需交互): https://docs.rapid7.com/metasploit/installing-the-metasploit-framework/"},
}

// normalize 补齐派生字段并做路径探测。
func normalize(t Tool) Tool {
	if t.Bin == "" {
		t.Bin = t.Name
	}
	t.ID = t.Bin
	if t.BinInZip == "" {
		t.BinInZip = t.Bin
	}
	t.Installable = t.Kind != KindManual
	return t
}

// humanInstall 给缺失项生成人类可读的安装提示(没有配方时回落到系统包名)。
func humanInstall(t Tool) string {
	switch t.Kind {
	case KindApt:
		return "apt install " + t.Apt
	case KindGitHub:
		return "GitHub Release: https://github.com/" + t.Repo + "/releases"
	case KindPip:
		return "pip install " + t.Pip
	}
	if t.Install != "" {
		return t.Install
	}
	return ""
}

// Catalog 返回目录的副本(含派生字段,未探测可用性)。
func Catalog() []Tool {
	out := make([]Tool, len(catalog))
	for i, t := range catalog {
		out[i] = normalize(t)
	}
	return out
}

// Lookup 按 id 取配方;ok=false 表示不在白名单内——这是特权助手唯一的
// 输入校验依据:凡是这里查不到的 id 一律拒绝执行。
func Lookup(id string) (Tool, bool) {
	for _, t := range catalog {
		n := normalize(t)
		if n.ID == id {
			return n, true
		}
	}
	return Tool{}, false
}

// AvailableInPath 该工具当前是否已在 PATH 内。
func (t Tool) AvailableInPath() bool {
	_, err := exec.LookPath(t.Bin)
	return err == nil
}

// Inventory 探测全部目录项(顺序与 catalog 一致)。
func Inventory() []Tool {
	out := make([]Tool, len(catalog))
	for i, t := range catalog {
		n := normalize(t)
		n.Available = false
		if _, err := exec.LookPath(n.Bin); err == nil {
			n.Available = true
			n.Install = ""
		} else {
			n.Install = humanInstall(n)
		}
		out[i] = n
	}
	return out
}

// Summary 注入 graph_overview / API 的紧凑形态:
// available 是可直接调用的二进制名清单;missing 带安装提示;
// hint 给模型一句话用法约定。
func Summary() map[string]any {
	inv := Inventory()
	avail := make([]string, 0, len(inv))
	missing := make([]map[string]string, 0, len(inv))
	for _, t := range inv {
		if t.Available {
			avail = append(avail, t.Bin)
		} else {
			missing = append(missing, map[string]string{"name": t.Name, "install": t.Install})
		}
	}
	return map[string]any{
		"available": avail,
		"missing":   missing,
		"hint":      "available 是本机 PATH 里可直接经 Bash 调用的工具;missing 里的工具本机没有,派意图选手段时以可用工具为准,不要臆测缺失工具的存在。需要缺失工具时在意图/小结里向用户提示安装命令,不要自行安装。",
	}
}

// SummaryText 纯文本形态(提示词注入用):一行可用、一行缺失。
func SummaryText() string {
	var b strings.Builder
	for _, t := range Inventory() {
		if t.Available {
			b.WriteString("✓ " + t.Name + "\n")
		} else {
			b.WriteString("✗ " + t.Name + "(未安装:" + t.Install + ")\n")
		}
	}
	return b.String()
}

// Arch 归一化为 Release 资产里用的架构名。
func Arch() string {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return runtime.GOARCH
	default:
		return runtime.GOARCH
	}
}

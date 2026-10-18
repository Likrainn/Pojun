// Package hosttools:宿主工具体检。
// 二进制部署形态下,ARTEX 直接跑在操作系统里——宿主装了什么渗透工具,
// worker 就能通过 Bash 工具直接调用什么。这里用 exec.LookPath 探测一份
// 常用工具清单(可用/缺失+安装提示),注入 graph_overview 让模型派意图时
// 知道家底,并通过 /api/tools/inventory 暴露给前端展示。
// LookPath 只扫 PATH,单次微秒级,无需缓存。
package hosttools

import (
	"os/exec"
	"strings"
)

// Tool 一条体检结果。
type Tool struct {
	Name      string `json:"name"`              // 工具名(展示用)
	Bin       string `json:"bin"`               // PATH 里探测的二进制名
	Available bool   `json:"available"`         // 是否在 PATH
	Purpose   string `json:"purpose"`           // 干什么用(给模型看)
	Install   string `json:"install,omitempty"` // 缺失时的安装提示
}

// catalog 常用渗透工具目录。Bin 留空 = 与 Name 相同;
// install 按 Debian/Ubuntu apt 给(与 deploy-linux.sh --with-tools 对齐),
// 不在 apt 源的给官方安装指引。
var catalog = []Tool{
	{Name: "nmap", Purpose: "端口扫描/服务指纹", Install: "apt install nmap"},
	{Name: "masscan", Purpose: "大规模端口快扫", Install: "apt install masscan"},
	{Name: "nuclei", Purpose: "基于模板的漏洞扫描(漏洞情报联动)", Install: "见 https://github.com/projectdiscovery/nuclei/releases 下载二进制,或 go install github.com/projectdiscovery/nuclei/v3/cmd/nuclei@latest"},
	{Name: "httpx", Purpose: "Web 存活/指纹批量探测(projectdiscovery 版)", Install: "go install github.com/projectdiscovery/httpx/cmd/httpx@latest"},
	{Name: "subfinder", Purpose: "子域名枚举", Install: "go install github.com/projectdiscovery/subfinder/v2/cmd/subfinder@latest"},
	{Name: "sqlmap", Purpose: "SQL 注入检测与利用", Install: "apt install sqlmap"},
	{Name: "ffuf", Purpose: "目录/参数模糊枚举", Install: "apt install ffuf"},
	{Name: "gobuster", Purpose: "目录/DNS/vhost 爆破", Install: "apt install gobuster"},
	{Name: "nikto", Purpose: "Web 服务器常见问题扫描", Install: "apt install nikto"},
	{Name: "whatweb", Purpose: "Web 指纹识别", Install: "apt install whatweb"},
	{Name: "hydra", Purpose: "在线口令爆破(ssh/登录表单等)", Install: "apt install hydra"},
	{Name: "john", Purpose: "离线口令破解", Install: "apt install john"},
	{Name: "hashcat", Purpose: "GPU 口令破解", Install: "apt install hashcat"},
	{Name: "msfconsole", Purpose: "Metasploit 框架(exploit/encoder)", Install: "见 https://docs.rapid7.com/metasploit/installing-the-metasploit-framework/"},
	{Name: "nc", Purpose: "TCP/UDP 连接与监听(反连交互)", Install: "apt install netcat-openbsd"},
	{Name: "curl", Purpose: "HTTP 请求与调试", Install: "apt install curl"},
	{Name: "dig", Purpose: "DNS 查询", Install: "apt install dnsutils"},
	{Name: "python3", Purpose: "脚本执行/自定义 PoC", Install: "apt install python3"},
	{Name: "openssl", Purpose: "TLS 证书检查/加解密", Install: "apt install openssl"},
	{Name: "ssh", Purpose: "登录验证/隧道", Install: "apt install openssh-client"},
	{Name: "testssl.sh", Purpose: "TLS 配置弱项扫描", Bin: "testssl", Install: "apt install testssl.sh"},
	{Name: "wafw00f", Purpose: "WAF 识别", Install: "pip install wafw00f"},
}

// Inventory 探测全部目录项(顺序与 catalog 一致)。
func Inventory() []Tool {
	out := make([]Tool, len(catalog))
	for i, t := range catalog {
		t.Bin = firstNonEmpty(t.Bin, t.Name)
		t.Available = false
		if _, err := exec.LookPath(t.Bin); err == nil {
			t.Available = true
			t.Install = ""
		}
		out[i] = t
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

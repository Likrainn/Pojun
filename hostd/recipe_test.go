package hostd

import (
	"strings"
	"testing"

	"github.com/chu0119/artex/hosttools"
)

// ValidateProxy 是唯一接受「外部字符串」的入口,注入面必须挡死。
func TestValidateProxy(t *testing.T) {
	good := []string{
		"",
		"http://10.0.0.14:7897",
		"https://user:pass@proxy.example.com:443",
		"socks5://127.0.0.1:1080",
		"socks5h://127.0.0.1:1080",
	}
	for _, s := range good {
		if _, err := ValidateProxy(s); err != nil {
			t.Errorf("应接受 %q,却报错: %v", s, err)
		}
	}

	bad := []string{
		"ftp://1.2.3.4:21",              // 不支持的 scheme
		"http://",                       // 缺 host
		"not-a-url",                     // 无 scheme
		"http://a:1\nAcquire::x=y",      // 换行注入
		"http://a:1 -o Foo::Bar=/tmp/x", // 空格注入(会把参数塞给 apt)
		"http://a:1\ttab",               // 制表符
	}
	for _, s := range bad {
		if _, err := ValidateProxy(s); err == nil {
			t.Errorf("应拒绝 %q,却通过了", s)
		}
	}
}

// 白名单是特权边界的核心:任何不在 catalog 里的 id 都必须查不到。
func TestLookupAllowlist(t *testing.T) {
	if _, ok := hosttools.Lookup("nmap"); !ok {
		t.Error("nmap 应在白名单内")
	}
	for _, bad := range []string{
		"", "rm", "sh", "/bin/sh", "../../etc/passwd",
		"nmap; rm -rf /", "nmap$(id)", "NMAP",
	} {
		if _, ok := hosttools.Lookup(bad); ok {
			t.Errorf("%q 不该在白名单内", bad)
		}
	}
}

// 命令必须是固定 argv,且包名只能来自 catalog —— 不经过 shell,不存在拼接注入。
func TestAptArgvUsesCatalogPackage(t *testing.T) {
	nmap, _ := hosttools.Lookup("nmap")

	argv, err := aptArgv(nmap, PkgApt, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(argv, " "); got != "apt-get install -y nmap" {
		t.Errorf("apt argv 不对: %s", got)
	}

	// 代理只应作为 -o Acquire::* 选项出现,不能变成独立命令
	argv, _ = aptArgv(nmap, PkgApt, "http://10.0.0.14:7897")
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "Acquire::http::Proxy=http://10.0.0.14:7897") {
		t.Errorf("代理未按 Acquire 选项注入: %s", joined)
	}
	if strings.Count(joined, "install") != 1 {
		t.Errorf("argv 结构异常: %s", joined)
	}

	// dnf 用另一个包名表
	argv, _ = aptArgv(nmap, PkgDnf, "")
	if got := strings.Join(argv, " "); got != "dnf install -y nmap" {
		t.Errorf("dnf argv 不对: %s", got)
	}
}

func TestAptArgvNoPkgMgr(t *testing.T) {
	nmap, _ := hosttools.Lookup("nmap")
	if _, err := aptArgv(nmap, PkgNone, ""); err == nil {
		t.Error("无包管理器时应报错")
	}
}

func TestGitHubAssetURL(t *testing.T) {
	nuclei, ok := hosttools.Lookup("nuclei")
	if !ok {
		t.Fatal("catalog 里应有 nuclei")
	}
	u := GitHubAssetURL(nuclei, "v3.4.10")
	want := "https://github.com/projectdiscovery/nuclei/releases/download/v3.4.10/nuclei_3.4.10_linux_"
	if !strings.HasPrefix(u, want) {
		t.Errorf("资产 URL 前缀不对: %s", u)
	}
	if strings.Contains(u, "vv") {
		t.Errorf("版本前缀 v 未剥离: %s", u)
	}
	if !strings.HasSuffix(u, ".zip") {
		t.Errorf("应为 zip 资产: %s", u)
	}
}

func TestPipArgvIncludesProxy(t *testing.T) {
	w, ok := hosttools.Lookup("wafw00f")
	if !ok {
		t.Fatal("catalog 里应有 wafw00f")
	}
	argv := pipArgv(w, "")
	if argv[len(argv)-1] != "wafw00f" {
		t.Errorf("末尾应是包名: %v", argv)
	}
	argv = pipArgv(w, "http://10.0.0.14:7897")
	if !strings.Contains(strings.Join(argv, " "), "10.0.0.14:7897") {
		t.Errorf("代理未传入: %v", argv)
	}
}

// manual 类工具必须不可一键安装,否则 hostd 会去执行不存在的配方。
func TestManualToolNotInstallable(t *testing.T) {
	msf, ok := hosttools.Lookup("msfconsole")
	if !ok {
		t.Fatal("catalog 里应有 msfconsole")
	}
	if msf.Installable {
		t.Error("msfconsole 应标记为不可一键安装")
	}
	if msf.Kind != hosttools.KindManual {
		t.Errorf("msfconsole kind 应为 manual,得到 %q", msf.Kind)
	}
}

// 新增的 ProjectDiscovery 系工具（naabu/dnsx/katana/tlsx）必须与既有三个
// 同构：资产名 {tool}_{ver}_linux_{arch}.zip（不带 v 前缀）。
// 模板写错的症状是「下载 404」，而用户只看到安装失败，看不出是资产名拼错。
func TestAssetURLForProjectDiscoveryFamily(t *testing.T) {
	for _, id := range []string{"nuclei", "httpx", "subfinder", "naabu", "dnsx", "katana", "tlsx"} {
		tool, ok := hosttools.Lookup(id)
		if !ok {
			t.Fatalf("%s 不在 catalog 里", id)
		}
		if tool.Kind != hosttools.KindGitHub {
			t.Errorf("%s kind 应为 github，得到 %q", id, tool.Kind)
		}
		u := GitHubAssetURL(tool, "v1.2.3")
		want := "https://github.com/projectdiscovery/" + id +
			"/releases/download/v1.2.3/" + id + "_1.2.3_linux_"
		if !strings.HasPrefix(u, want) {
			t.Errorf("%s URL 不对: %s", id, u)
		}
		if !strings.HasSuffix(u, ".zip") {
			t.Errorf("%s URL 后缀不对: %s", id, u)
		}
	}
}

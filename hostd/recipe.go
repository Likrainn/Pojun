// Package hostd:ARTEX 的特权安装助手(artex-hostd)。
//
// 为什么需要它:主服务 systemd 单元带 NoNewPrivileges=true + ProtectSystem=strict
// + CapabilityBoundingSet=CAP_NET_BIND_SERVICE,进程无法提权、/usr 只读——所以
// 「一键安装」不可能由 Web 服务自己完成。hostd 以 root 常驻,只经一个 Unix socket
// 接受请求,且**只认 hosttools 白名单里的工具 id**,永远不接受自由命令。
//
// 攻击面收敛:无 TCP 端口(仅 AF_UNIX);socket 0600/0660 且归属运行用户;每个动作
// 都由 catalog 的固定 argv 拼出,代理地址经 URL 校验;所有执行的命令落审计日志。
package hostd

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/chu0119/artex/hosttools"
)

// PkgMgr 系统包管理器。
type PkgMgr string

const (
	PkgApt PkgMgr = "apt"
	PkgDnf PkgMgr = "dnf"
	PkgNone PkgMgr = ""
)

// DetectPkgMgr 探测本机包管理器(只认这两个;其它发行版走 GitHub/pip 配方)。
func DetectPkgMgr() PkgMgr {
	for _, p := range []struct {
		mgr  PkgMgr
		path string
	}{{PkgApt, "/usr/bin/apt-get"}, {PkgApt, "/usr/local/bin/apt-get"}, {PkgDnf, "/usr/bin/dnf"}} {
		if st, err := os.Stat(p.path); err == nil && !st.IsDir() {
			return p.mgr
		}
	}
	return PkgNone
}

// ValidateProxy 校验代理地址。只允许 http/https/socks5,且必须有 host:port——
// 因为代理串会作为 argv/env 传给 apt/子进程,必须挡住换行/空格等注入面。
func ValidateProxy(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.ContainsAny(raw, "\n\r\t ") {
		return "", fmt.Errorf("代理地址含非法空白字符")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("代理地址无法解析: %v", err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", fmt.Errorf("代理仅支持 http/https/socks5,收到 %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("代理地址缺少 host:port")
	}
	return raw, nil
}

// APTProxyArgs 把代理转成 apt 的 Acquire 选项(不设全局 apt.conf,避免持久污染)。
func APTProxyArgs(proxy string) []string {
	if proxy == "" {
		return nil
	}
	return []string{
		"-o", "Acquire::http::Proxy=" + proxy,
		"-o", "Acquire::https::Proxy=" + proxy,
	}
}

// aptArgv 构造 apt 安装命令。pkg 来自 catalog(非用户输入),proxy 已校验。
func aptArgv(tool hosttools.Tool, mgr PkgMgr, proxy string) ([]string, error) {
	var pkg string
	switch mgr {
	case PkgApt:
		pkg = tool.Apt
	case PkgDnf:
		pkg = tool.Dnf
	default:
		return nil, fmt.Errorf("%s 需要系统包管理器(apt/dnf),本机未检测到", tool.Name)
	}
	if pkg == "" {
		return nil, fmt.Errorf("%s 没有适配本机包管理器的包名", tool.Name)
	}
	if mgr == PkgApt {
		argv := append([]string{"apt-get"}, APTProxyArgs(proxy)...)
		return append(argv, "install", "-y", pkg), nil
	}
	argv := []string{"dnf"}
	if proxy != "" {
		argv = append(argv, "--setopt=proxy="+proxy)
	}
	return append(argv, "install", "-y", pkg), nil
}

// aptUpdateArgv apt 索引刷新(apt 源随时间失效,装之前先 update 一次)。
func aptUpdateArgv(mgr PkgMgr, proxy string) []string {
	if mgr == PkgApt {
		argv := append([]string{"apt-get"}, APTProxyArgs(proxy)...)
		return append(argv, "update")
	}
	return []string{"dnf", "makecache"}
}

// pipArgv 构造 pip 安装命令。优先 pipx(隔离环境,避免 PEP668 externally-managed
// 报错),没有则回落到 python3 -m pip --break-system-packages。
func pipArgv(tool hosttools.Tool, proxy string) []string {
	if _, err := os.Stat("/usr/bin/pipx"); err == nil {
		argv := []string{"pipx", "install"}
		if proxy != "" {
			argv = append(argv, "--pip-args=--proxy="+proxy)
		}
		return append(argv, tool.Pip)
	}
	argv := []string{"python3", "-m", "pip", "install", "--break-system-packages"}
	if proxy != "" {
		argv = append(argv, "--proxy", proxy)
	}
	return append(argv, tool.Pip)
}

// GitHubAssetURL 由资产模板拼出下载地址。tag 形如 v3.4.10,资产名里不带 v。
func GitHubAssetURL(tool hosttools.Tool, tag string) string {
	ver := strings.TrimPrefix(tag, "v")
	asset := strings.ReplaceAll(tool.AssetTmpl, "{ver}", ver)
	asset = strings.ReplaceAll(asset, "{arch}", hosttools.Arch())
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", tool.Repo, tag, asset)
}

// GitHubAPITagURL 取最新 tag 的 API 地址。
func GitHubAPITagURL(tool hosttools.Tool) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", tool.Repo)
}

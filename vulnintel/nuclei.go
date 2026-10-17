package vulnintel

// nuclei-templates 模板树管理与扫描工具支撑:
// - EnsureTemplates:模板树不存在则 git clone --depth 1,存在则 git pull(fast-forward)
// - 模板树目录默认 <data>/nuclei-templates,由 server 传入
// - FindNucleiTemplates(见 poc.go)按 CVE 找模板相对路径
//
// 刻意不引 go-git:git 命令行可用性在运行镜像里有保证(Dockerfile 装 git),
// 且 --depth 1 的浅克隆用命令行最直接。

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TemplatesDir 是模板树位置(默认可执行文件旁 nuclei-templates/)。
func TemplatesDir(dataDir string) string {
	return filepath.Join(dataDir, "nuclei-templates")
}

// EnsureTemplates clones or updates the template tree. Best-effort: returns
// error but callers log it — scan_with_nuclei degrades to "templates
// unavailable" messaging rather than failing the sync.
func EnsureTemplates(ctx context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		// update: fetch + fast-forward only, ignore local drift
		cmd := exec.CommandContext(ctx, "git", "-C", dir, "pull", "--ff-only", "--quiet")
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("nuclei templates pull: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--quiet",
		"https://github.com/projectdiscovery/nuclei-templates.git", dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nuclei templates clone: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// NucleiInstalled reports whether the nuclei binary is on PATH.
func NucleiInstalled() bool {
	_, err := exec.LookPath("nuclei")
	return err == nil
}

// ScanArgs is scan_with_nuclei's validated input.
type ScanArgs struct {
	Target       string        // URL/host to scan
	CVEID        string        // optional: restrict to templates mentioning this CVE
	Severity     string        // optional: comma list critical,high,medium,low
	TimeoutTotal time.Duration // whole-run ceiling (default 10m, cap 30m)
}

// BuildNucleiCmd assembles the nuclei invocation. Templates restricted to the
// local tree (-t <dir>) so we never trigger nuclei's own template download
// (offline-friendly, version-pinned); -silent keeps output parseable; JSONL
// output goes to stdout for the agent to read. The returned cancel MUST be
// called by the caller after the run (it bounds the whole-run ceiling).
func BuildNucleiCmd(ctx context.Context, templatesDir string, a ScanArgs) (*exec.Cmd, context.CancelFunc, error) {
	if strings.TrimSpace(a.Target) == "" {
		return nil, nil, fmt.Errorf("target 不能为空")
	}
	if _, err := os.Stat(filepath.Join(templatesDir, ".git")); err != nil {
		return nil, nil, fmt.Errorf("模板树不可用(%s):先等同步器完成初始化", templatesDir)
	}
	timeout := a.TimeoutTotal
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	if timeout > 30*time.Minute {
		timeout = 30 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := []string{
		"-target", a.Target,
		"-t", templatesDir,
		"-jsonl",
		"-silent",
		"-no-color",
		"-disable-update-check",
	}
	if cve := strings.TrimSpace(a.CVEID); cve != "" {
		templates := FindNucleiTemplates(templatesDir, cve)
		if len(templates) == 0 {
			cancel()
			return nil, nil, fmt.Errorf("模板树中没有命中 %s 的模板(树可能过期,等下轮同步)", cve)
		}
		for _, rel := range templates {
			args = append(args, "-t", filepath.Join(templatesDir, filepath.FromSlash(rel)))
		}
	}
	if sev := strings.TrimSpace(a.Severity); sev != "" {
		args = append(args, "-severity", sev)
	}
	return exec.CommandContext(runCtx, "nuclei", args...), cancel, nil
}

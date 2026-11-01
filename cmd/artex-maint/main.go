// artex-maint 是离线维护工具（v0.4.13）：对已有部署做一次性数据修补，
// 不经过 Web 登录。两类操作：
//
//	artex-maint scan-artifacts  --config /opt/artex/config.json
//	 扫描所有任务工作目录里的历史图片，补登记为证据附件（与 v0.4.10 的
//	 POST /api/tasks/{id}/artifacts/scan 同逻辑，用于批量补齐老任务）。
//
//	artex-maint reset-prompts --config /opt/artex/config.json [--force]
//	 把内置 agent 提示词更新到当前二进制内置版本（老库的 prompt 是首次安装
//	 时的旧版，新版本不会自动覆盖）。默认只报告差异；--force 才写回（会覆盖
//	 该 agent 的自定义编辑，慎用）。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chu0119/artex/agent"
	"github.com/chu0119/artex/db"
)

const (
	maxFileBytes = 20 << 20
	maxTaskCount = 60
	maxTaskBytes = 200 << 20
	artifactsDir = "artifacts"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: artex-maint <scan-artifacts|reset-prompts> --config <config.json> [--force]")
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	cfgPath := fs.String("config", "/opt/artex/config.json", "ARTEX config.json 路径")
	dataDir := fs.String("data", "", "数据目录（默认取 config 目录下的 data）")
	force := fs.Bool("force", false, "reset-prompts: 覆盖自定义编辑，直接写回内置默认")
	_ = fs.Parse(os.Args[2:])

	dsn, err := loadDSN(*cfgPath)
	if err != nil {
		fail(err)
	}
	pg, err := db.Open(dsn)
	if err != nil {
		fail(fmt.Errorf("连接数据库失败：%w", err))
	}
	if *dataDir == "" {
		*dataDir = filepath.Join(filepath.Dir(*cfgPath), "data")
	}
	if err := pg.EnsureTaskArtifactsTable(); err != nil {
		fail(fmt.Errorf("建表失败：%w", err))
	}

	switch cmd {
	case "scan-artifacts":
		scanArtifacts(pg, *dataDir)
	case "reset-prompts":
		resetPrompts(pg, *force)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "[x] %v\n", err)
	os.Exit(1)
}

// loadDSN 从 config.json 的 database 段拼 DSN（与服务端同源）。
func loadDSN(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取配置失败：%w", err)
	}
	var c struct {
		Database struct {
			Host     string `json:"host"`
			Port     int    `json:"port"`
			User     string `json:"user"`
			Password string `json:"password"`
			DBName   string `json:"dbname"`
			SSLMode  string `json:"sslmode"`
		} `json:"database"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return "", err
	}
	d := c.Database
	if d.Host == "" || d.User == "" || d.DBName == "" {
		return "", fmt.Errorf("config.json 缺少 database 配置")
	}
	if d.Port == 0 {
		d.Port = 5432
	}
	if d.SSLMode == "" {
		d.SSLMode = "disable"
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode), nil
}

// scanArtifacts 扫描所有任务目录并补登记图片（去重/限量/忽略 artifacts 自身）。
func scanArtifacts(pg *db.DB, dataDir string) {
	tasks, err := pg.ListTasks()
	if err != nil {
		fail(fmt.Errorf("读取任务失败：%w", err))
	}
	totalAdded, totalSkipped := 0, 0
	for _, t := range tasks {
		taskID := strconv.FormatInt(t.ID, 10)
		root := filepath.Join(dataDir, "tasks", taskID)
		artDir := filepath.Join(root, artifactsDir)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		existing := map[string]bool{}
		if list, e := pg.ListTaskArtifacts(taskID); e == nil {
			for _, a := range list {
				existing[a.SHA256] = true
			}
		}
		count, bytesUsed, _ := pg.CountTaskArtifacts(taskID)
		added, skipped := 0, 0
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				if path == artDir {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".png", ".jpg", ".jpeg", ".gif", ".webp":
			default:
				return nil
			}
			info, e := d.Info()
			if e != nil || info.Size() <= 0 || info.Size() > maxFileBytes {
				return nil
			}
			if added >= maxTaskCount || count+added >= maxTaskCount {
				return nil
			}
			if bytesUsed+info.Size() > maxTaskBytes {
				return nil
			}
			sha, e := fileSHA(path)
			if e != nil {
				return nil
			}
			if existing[sha] {
				skipped++
				return nil
			}
			if e := register(pg, dataDir, taskID, path, root, sha, info.Size()); e != nil {
				fmt.Printf("  [warn] task %s %s: %v\n", taskID, filepath.Base(path), e)
				return nil
			}
			existing[sha] = true
			bytesUsed += info.Size()
			added++
			return nil
		})
		if added > 0 || skipped > 0 {
			fmt.Printf("[task %s] 补登记 %d 张，跳过重复 %d 张\n", taskID, added, skipped)
		}
		totalAdded += added
		totalSkipped += skipped
	}
	fmt.Printf("\n完成：共补登记 %d 张，跳过 %d 张\n", totalAdded, totalSkipped)
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func register(pg *db.DB, dataDir, taskID, path, taskRoot, sha string, size int64) error {
	base := filepath.Base(path)
	name := base
	if decoded, err := url.QueryUnescape(base); err == nil && decoded != "" {
		name = decoded
	}
	rel, _ := filepath.Rel(taskRoot, path)
	dstRel := filepath.Join("tasks", taskID, artifactsDir,
		fmt.Sprintf("%d-%s", time.Now().UnixNano(), sanitize(name)))
	dst := filepath.Join(dataDir, dstRel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return err
	}
	kind, mime := kindByExt(name)
	a := &db.TaskArtifact{
		TaskID: taskID, Name: name, Kind: kind, Mime: mime,
		SizeBytes: size, SHA256: sha, Note: "工作目录历史文件：" + rel,
		Storage: dstRel,
	}
	if err := pg.InsertTaskArtifact(context.Background(), a); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

func sanitize(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}

func kindByExt(name string) (string, string) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image", "image/png"
	case ".jpg", ".jpeg":
		return "image", "image/jpeg"
	case ".gif":
		return "image", "image/gif"
	case ".webp":
		return "image", "image/webp"
	}
	return "other", "application/octet-stream"
}

// resetPrompts 把内置 agent 提示词与当前二进制版本比对，可选写回。
func resetPrompts(pg *db.DB, force bool) {
	seeds := agent.BuiltinPromptSeeds()
	// reporter / retester 不在 BuiltinPromptSeeds 里（它们由 server 在
	// first-insert 时 seed 自己的常量），维护时一并纳入比对与写回。
	seeds["reporter"] = agent.ReporterDefaultPrompt
	seeds["retester"] = agent.RetesterDefaultPrompt
	list, err := pg.ListAgents()
	if err != nil {
		fail(fmt.Errorf("读取 agents 失败：%w", err))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	changed := 0
	for _, a := range list {
		tmpl, ok := seeds[a.Key]
		if !ok {
			continue
		}
		cur, err := pg.CurrentPrompt(a.ID)
		if err != nil {
			cur = ""
		}
		if cur == tmpl {
			fmt.Printf("[%s] 已是最新\n", a.Key)
			continue
		}
		changed++
		if !force {
			fmt.Printf("[%s] 与内置版本不同（当前 %d 字节，内置 %d 字节）；加 --force 写回\n",
				a.Key, len(cur), len(tmpl))
			continue
		}
		ver, err := pg.ResetPromptToDefault(a.ID, tmpl)
		if err != nil {
			fmt.Printf("[%s] 写回失败：%v\n", a.Key, err)
			continue
		}
		fmt.Printf("[%s] 已更新到内置版本 v%d\n", a.Key, ver)
	}
	fmt.Printf("\n完成：%d 个 agent 有差异\n", changed)
}

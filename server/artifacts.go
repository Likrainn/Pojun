package server

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chu0119/artex/db"
)

// 证据附件限额（v0.5.0）。默认"样本策略"：worker 只保存证明性样本，全量
// 数据留在任务工作目录、不随报告分发。限额防的是误存全量 PII/超大文件。
const (
	maxArtifactFileBytes = 20 << 20  // 单文件 20MB
	maxTaskArtifactCount = 100       // 每任务最多 100 个
	maxTaskArtifactBytes = 200 << 20 // 每任务总量 200MB
)

// artifactKindByExt 按扩展名推断附件类型（报告里分区展示用）。
func artifactKindByExt(name string) (kind, mime string) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image", "image/png"
	case ".jpg", ".jpeg":
		return "image", "image/jpeg"
	case ".gif":
		return "image", "image/gif"
	case ".webp":
		return "image", "image/webp"
	case ".json":
		return "data", "application/json"
	case ".csv":
		return "data", "text/csv"
	case ".txt", ".log":
		return "log", "text/plain"
	case ".html", ".htm":
		return "html", "text/html"
	case ".xml":
		return "data", "application/xml"
	case ".pdf":
		return "data", "application/pdf"
	}
	return "other", "application/octet-stream"
}

// sanitizeArtifactName 去掉路径分隔符与控制字符，限制长度。
func sanitizeArtifactName(name string) string {
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		name = "artifact"
	}
	if len([]rune(name)) > 120 {
		name = string([]rune(name)[:120])
	}
	return name
}

// taskArtifactsDir 任务的附件目录（data/tasks/<taskID>/artifacts）。
func (s *Server) taskArtifactsDir(taskID string) string {
	return filepath.Join(s.m.dir, "tasks", taskID, "artifacts")
}

// RecordArtifact 把任务工作目录里的一个文件登记为证据附件：校验路径边界与
// 限额 → 计算 sha256（同任务同内容去重）→ 复制到任务附件目录 → 落库。
// relPath 相对 runDir（worker 的工作目录）；必须落在 tasks/<taskID>/ 之内。
func (s *Server) RecordArtifact(taskID, findingID int64, runDir, relPath, name, kind, sourceURL, note string) (*db.TaskArtifact, error) {
	pg := s.m.PG()
	if pg == nil {
		return nil, fmt.Errorf("PG 不可用")
	}
	taskKey := strconv.FormatInt(taskID, 10)
	if strings.TrimSpace(relPath) == "" {
		return nil, fmt.Errorf("path 不能为空")
	}
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return nil, fmt.Errorf("path 必须是工作目录内的相对路径")
	}
	abs := filepath.Join(runDir, clean)
	// 路径边界：必须落在 tasks/<taskID>/ 之下（防穿越到其他任务/data 其他区域）。
	// 两边都做 symlink 解析后再比较，避免用链接绕出任务目录。
	taskRoot := filepath.Join(s.m.dir, "tasks", taskKey)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if resolved, err := filepath.EvalSymlinks(taskRoot); err == nil {
		taskRoot = resolved
	}
	relToTask, err := filepath.Rel(taskRoot, abs)
	if err != nil || strings.HasPrefix(relToTask, "..") {
		return nil, fmt.Errorf("path 超出任务目录范围")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("文件不存在或不可读：%w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("只支持普通文件")
	}
	if info.Size() <= 0 {
		return nil, fmt.Errorf("文件为空")
	}
	if info.Size() > maxArtifactFileBytes {
		return nil, fmt.Errorf("文件超过单附件上限（20MB）：%d 字节", info.Size())
	}
	if count, total, err := pg.CountTaskArtifacts(taskKey); err == nil {
		if count >= maxTaskArtifactCount {
			return nil, fmt.Errorf("本任务附件已达上限（%d 个），请合并或删除后再存", maxTaskArtifactCount)
		}
		if total+info.Size() > maxTaskArtifactBytes {
			return nil, fmt.Errorf("本任务附件总量将超过上限（200MB）")
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败：%w", err)
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])

	// 去重：同任务同内容直接返回已有登记，不重复落盘。
	if list, err := pg.ListTaskArtifacts(taskKey); err == nil {
		for i := range list {
			if list[i].SHA256 == sha && list[i].SizeBytes == info.Size() {
				return &list[i], nil
			}
		}
	}

	if strings.TrimSpace(name) == "" {
		name = filepath.Base(clean)
	}
	name = sanitizeArtifactName(name)
	autoKind, autoMime := artifactKindByExt(name)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" || kind == "auto" {
		kind = autoKind
	}
	dir := s.taskArtifactsDir(taskKey)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建附件目录失败：%w", err)
	}
	storage := filepath.Join("tasks", taskKey, "artifacts",
		fmt.Sprintf("%d-%s", time.Now().UnixNano(), name))
	dst := filepath.Join(s.m.dir, storage)
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return nil, fmt.Errorf("写入附件失败：%w", err)
	}
	a := &db.TaskArtifact{
		TaskID: taskKey, FindingID: findingID, Name: name, Kind: kind, Mime: autoMime,
		SizeBytes: int64(len(data)), SHA256: sha, SourceURL: sourceURL,
		Note: note, Storage: storage,
	}
	if err := pg.InsertTaskArtifact(context.Background(), a); err != nil {
		_ = os.Remove(dst)
		return nil, fmt.Errorf("登记附件失败：%w", err)
	}
	return a, nil
}

// listTaskArtifacts 任务附件列表（不含存储路径）。
func (s *Server) listTaskArtifacts(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	pg := s.m.PG()
	if pg == nil {
		writeErr(w, 500, "PG 不可用")
		return
	}
	list, err := pg.ListTaskArtifacts(taskID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []db.TaskArtifact{}
	}
	writeJSON(w, 200, map[string]any{"artifacts": list})
}

// downloadTaskArtifact 下载单个附件（前端预览/单独取证用）。
func (s *Server) downloadTaskArtifact(w http.ResponseWriter, r *http.Request) {
	pg := s.m.PG()
	if pg == nil {
		writeErr(w, 500, "PG 不可用")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("aid"), 10, 64)
	if err != nil {
		writeErr(w, 400, "invalid artifact id")
		return
	}
	a, err := pg.GetTaskArtifact(id)
	if err != nil || a == nil {
		writeErr(w, 404, "artifact not found")
		return
	}
	if a.TaskID != r.PathValue("id") {
		writeErr(w, 404, "artifact not found")
		return
	}
	abs := filepath.Join(s.m.dir, a.Storage)
	w.Header().Set("Content-Type", a.Mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeArtifactName(a.Name)))
	http.ServeFile(w, r, abs)
}

// exportTaskArtifacts 把任务全部附件打成 zip：artifacts/<name> + manifest.json。
// manifest 记录来源 URL、采集时间、sha256 与说明——SRC 取证提交需要这些。
func (s *Server) exportTaskArtifacts(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	pg := s.m.PG()
	if pg == nil {
		writeErr(w, 500, "PG 不可用")
		return
	}
	list, err := pg.ListTaskArtifacts(taskID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if len(list) == 0 {
		writeErr(w, 404, "该任务没有证据附件")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="task-%s-artifacts.zip"`, taskID))
	zw := zip.NewWriter(w)
	defer zw.Close()
	manifest := make([]map[string]any, 0, len(list))
	seen := map[string]int{}
	for _, a := range list {
		abs := filepath.Join(s.m.dir, a.Storage)
		data, err := os.ReadFile(abs)
		if err != nil {
			continue // 文件丢失的登记跳过，manifest 里如实缺席
		}
		name := a.Name
		if n := seen[name]; n > 0 {
			ext := filepath.Ext(name)
			name = strings.TrimSuffix(name, ext) + fmt.Sprintf("-%d", n+1) + ext
		}
		seen[a.Name]++
		fw, err := zw.Create("artifacts/" + name)
		if err != nil {
			continue
		}
		_, _ = fw.Write(data)
		manifest = append(manifest, map[string]any{
			"id": a.ID, "finding_id": a.FindingID, "file": "artifacts/" + name, "name": a.Name, "kind": a.Kind,
			"mime": a.Mime, "size_bytes": a.SizeBytes, "sha256": a.SHA256,
			"source_url": a.SourceURL, "note": a.Note,
			"created_at": a.CreatedAt.Format(time.RFC3339),
		})
	}
	mf, _ := json.MarshalIndent(map[string]any{
		"task_id":     taskID,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"policy":      "样本策略：仅保存证明性样本，全量数据不回传；附件随报告分发前请确认脱敏。",
		"artifacts":   manifest,
	}, "", "  ")
	if fw, err := zw.Create("manifest.json"); err == nil {
		_, _ = fw.Write(mf)
	}
}

// artifactImageData 读一张图片附件（报告 docx 嵌图用）。
func (s *Server) artifactImageData(a *db.TaskArtifact) ([]byte, bool) {
	if a == nil || a.Kind != "image" {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(s.m.dir, a.Storage))
	if err != nil {
		return nil, false
	}
	return data, true
}

// artifactListJSON 供报告生成时把附件清单写进"附件"章节。
func artifactListJSON(list []db.TaskArtifact) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{
			"id": a.ID, "finding_id": a.FindingID, "name": a.Name, "kind": a.Kind, "size_bytes": a.SizeBytes,
			"note": a.Note, "source_url": a.SourceURL,
		})
	}
	return out
}

// 宿主环境 API:体检 + 一键安装。
//
// 「一键安装」的实现在特权助手 artex-hostd 里(见 hostd 包)——主服务受
// NoNewPrivileges + ProtectSystem=strict 限制无法提权,只能经 Unix socket 把
// 请求转过去。本文件是这层转发 + 前端契约。
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/chu0119/artex/hosttools"
)

// 默认 socket 路径,与 hostd 单元里的 -socket 一致;可用环境变量覆盖。
const defaultHostdSocket = "/run/artex/hostd.sock"

func hostdSocket() string {
	if p := strings.TrimSpace(os.Getenv("ARTEX_HOSTD_SOCKET")); p != "" {
		return p
	}
	return defaultHostdSocket
}

// hostdClient 只经 Unix socket 访问助手:没有任何 TCP 面。
func hostdClient() *http.Client {
	sock := hostdSocket()
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
		},
	}
}

// hostdProbe 探测助手是否在线,返回 (在线, 原因)。原因用于前端解释为什么不能装。
func (s *Server) hostdProbe(ctx context.Context) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://hostd/healthz", nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := hostdClient().Do(req)
	if err != nil {
		return false, "宿主助手未运行（未安装 artex-hostd，或当前是 Docker 部署）"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, "宿主助手响应异常: HTTP " + resp.Status
	}
	return true, ""
}

// installProxy 读「安装期出网代理」设置。与运行期的目标代理/LLM 代理无关:
// 它只作用于 apt/curl/GitHub 下载。
func (s *Server) installProxy() string {
	if s.m == nil || s.m.pg == nil {
		return ""
	}
	v, _, _ := s.m.pg.GetSetting(settingInstallProxy)
	return strings.TrimSpace(v)
}

// maintenanceProxy 维护类出网（检查/下载更新、安装宿主工具）走哪个代理。
//
// 优先「装机代理」——它本来就是为「连 GitHub 拉东西」准备的；老部署没配过它时
// 回落到全局代理，保证行为不变。两者语义不同：全局代理是给**目标流量**隐藏源 IP
// 用的，拿它去拉更新只是历史上没有别的选择。
func (s *Server) maintenanceProxy() string {
	if p := s.installProxy(); p != "" {
		return p
	}
	if s.m == nil {
		return ""
	}
	return s.m.GlobalProxy()
}

// hostenvStatus 体检页的总入口:工具清单 + 是否支持一键安装。
func (s *Server) hostenvStatus(w http.ResponseWriter, r *http.Request) {
	available, reason := s.hostdProbe(r.Context())
	out := map[string]any{
		"tools": hosttools.Inventory(),
		"deploy_mode": deployMode(),
		"installer": map[string]any{
			"available": available,
			"reason":    reason,
			"socket":    hostdSocket(),
			"proxy":     s.installProxy(), // 回显当前安装代理,前端可编辑
		},
	}
	writeJSON(w, 200, out)
}

// hostenvInstall 请求安装一个工具,转发给助手。
func (s *Server) hostenvInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		writeErr(w, 400, "缺少工具 id")
		return
	}
	// 白名单在主服务侧先挡一道(助手侧还会再挡),避免无谓的跨进程调用。
	if _, ok := hosttools.Lookup(req.ID); !ok {
		writeErr(w, 400, "未知工具: "+req.ID)
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"tool_id": req.ID,
		"proxy":   s.installProxy(),
	})
	body, code, err := s.hostdCall(r.Context(), http.MethodPost, "/jobs", payload)
	if err != nil {
		writeErr(w, 503, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// hostenvJob 查询安装任务进度(前端轮询)。
func (s *Server) hostenvJob(w http.ResponseWriter, r *http.Request) {
	body, code, err := s.hostdCall(r.Context(), http.MethodGet, "/jobs/"+r.PathValue("id"), nil)
	if err != nil {
		writeErr(w, 503, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// hostenvCancel 取消安装任务。
func (s *Server) hostenvCancel(w http.ResponseWriter, r *http.Request) {
	body, code, err := s.hostdCall(r.Context(), http.MethodPost, "/jobs/"+r.PathValue("id")+"/cancel", nil)
	if err != nil {
		writeErr(w, 503, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// hostdCall 把一个请求转给助手,原样带回状态码与响应体。
func (s *Server) hostdCall(ctx context.Context, method, path string, payload []byte) ([]byte, int, error) {
	var bodyReader io.Reader
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "http://hostd"+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hostdClient().Do(req)
	if err != nil {
		return nil, 0, errConnectHostd(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return body, resp.StatusCode, nil
}

type hostdUnavailable struct{ msg string }

func (e hostdUnavailable) Error() string { return e.msg }

func errConnectHostd(err error) error {
	return hostdUnavailable{"宿主助手不可用（未安装 artex-hostd，或当前是 Docker 部署）: " + err.Error()}
}

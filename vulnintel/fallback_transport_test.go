package vulnintel

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 代理传输层失败时必须回退直连。
//
// 回归背景（2026-10-21 实测）：出网代理 10.10.70.14:7897 对
// api.github.com 正常（200），但对 www.cisa.gov / services.nvd.nist.gov
// 会「CONNECT 返回 200、随后 TLS 握手 unexpected eof」——隧道建起来却不
// 转发数据。同一轮同步里 GitHub 源成功、NVD/KEV 源全挂，而直连这两个源
// 本来是通的。代理在这里不只没用，还把能通的路堵死了。
func TestFallbackTransportFallsBackToDirect(t *testing.T) {
	// 直连目标：一个正常返回 200 的 httptest 服务。
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("direct-ok"))
	}))
	defer direct.Close()

	// 代理：CONNECT 成功但随后立刻断开 —— 复现真实故障的形态。
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			w.WriteHeader(http.StatusOK)
			return
		}
		// 普通代理请求：也断掉。
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close()
				return
			}
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()

	tr, err := newFallbackTransport(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: tr}

	resp, err := client.Get(direct.URL)
	if err != nil {
		t.Fatalf("fallback did not reach the direct server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// HTTP 层的错误**不能**回退：404/403/429 是源本身的问题，换条路还是
// 同一个答案，回退只会把一次请求变成两次，白耗配额（NVD 匿名只有
// 5 次/30 秒）。
func TestFallbackTransportDoesNotRetryHTTPErrors(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer proxy.Close()

	tr, err := newFallbackTransport(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: tr}

	resp, err := client.Get("http://example.invalid/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (proxy answer must pass through)", resp.StatusCode)
	}
}

// 传输层错误的分类必须准：只有「换条路可能就好」的才回退。
func TestIsTransportFailure(t *testing.T) {
	fatal := []string{
		`Get "https://x": EOF`,
		`Get "https://x": unexpected EOF`,
		`dial tcp 1.2.3.4:443: connect: connection refused`,
		`read tcp: connection reset by peer`,
		`net/http: TLS handshake timeout`,
		`Get "https://x": i/o timeout`,
	}
	for _, msg := range fatal {
		if !isTransportFailure(errString(msg)) {
			t.Errorf("should be a transport failure: %s", msg)
		}
	}
	benign := []string{
		`Get "https://x": 404 Not Found`,
		`unexpected status 403`,
		`Get "https://x": 429 Too Many Requests`,
		`invalid character 'x'`,
	}
	for _, msg := range benign {
		if isTransportFailure(errString(msg)) {
			t.Errorf("should NOT be a transport failure: %s", msg)
		}
	}
	if isTransportFailure(nil) {
		t.Error("nil must not be a transport failure")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// 空代理 = 直连，不该引入任何额外层。
func TestFallbackTransportWithoutProxyIsDirect(t *testing.T) {
	tr, err := newFallbackTransport("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.(*fallbackTransport); ok {
		t.Error("empty proxy should not build a fallback transport")
	}
	// 也不该报错或返回 nil
	if tr == nil {
		t.Fatal("nil transport")
	}
}

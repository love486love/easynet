package probe

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/love486love/easynet/internal/portal"
)

const (
	firstChunk = 1 << 10 // 首段只读 1 KB
	maxBody    = 8 << 10 // 疑似拦截页时最多再读到 8 KB
	userAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/155.0.0.0 Safari/537.36"
)

// Client 发起探针请求。不会跟随重定向，只读取 Location。
type Client struct {
	http *http.Client
}

// New 返回默认探针客户端（整体超时 6s）。
func New() *Client {
	return &Client{http: &http.Client{
		Timeout: 6 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Check 请求探针地址并给出判定结果。
// expect 为正常响应中必然出现的正文片段，空串表示退回特征排除法。
func (c *Client) Check(ctx context.Context, probeURL string, allowedHosts []string, expect string) (portal.State, Evidence, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return portal.StateUnknown, Evidence{}, err
	}
	req.Header.Set("Range", "bytes=0-1023")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return portal.StateUnknown, Evidence{}, err
	}
	defer resp.Body.Close()

	ev := Evidence{
		StatusCode: resp.StatusCode,
		Location:   resp.Header.Get("Location"),
		Server:     resp.Header.Get("Server"),
	}

	head := make([]byte, firstChunk)
	n, err := io.ReadFull(resp.Body, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return portal.StateUnknown, ev, err
	}
	ev.Body = head[:n]

	// 配置了 expect 但首段没命中时，再多读一段，避免特征恰好落在 1 KB 之后。
	needMore := expect != "" && !bytesContainsFold(ev.Body, expect)
	if suspicious(resp) || needMore {
		rest := make([]byte, maxBody-firstChunk)
		m, err := io.ReadFull(resp.Body, rest)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return portal.StateUnknown, ev, err
		}
		ev.Body = append(ev.Body, rest[:m]...)
	}

	return Judge(probeURL, allowedHosts, expect, ev), ev, nil
}

func bytesContainsFold(haystack []byte, needle string) bool {
	return strings.Contains(strings.ToLower(string(haystack)), strings.ToLower(needle))
}

// suspicious 判断是否值得继续读正文：短页面、长度未知、或网关特征。
func suspicious(resp *http.Response) bool {
	if resp.ContentLength < 0 || resp.ContentLength < 16<<10 {
		return true
	}
	s := strings.ToLower(resp.Header.Get("Server"))
	return strings.Contains(s, "netengine") || strings.Contains(s, "drcom")
}

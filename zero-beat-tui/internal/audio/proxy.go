package audio

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// headerProxy 是只监听回环地址的 header 注入代理。
//
// 背景：ffmpeg 只有 -headers 参数，把 Cookie 直接放进命令行会让同机用户
// 通过 ps 看到登录态。这里让 ffmpeg 读 http://127.0.0.1:<port>/<token>，
// Cookie/Referer 只保存在进程内存里，由代理转发到真正的音频 CDN。
type headerProxy struct {
	server  *http.Server
	ln      net.Listener
	url     string
	target  string
	headers map[string]string
	client  *http.Client
	once    sync.Once
}

func newHeaderProxy(target string, headers map[string]string) (*headerProxy, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, errors.New("audio: 代理目标为空")
	}
	tokenRaw := make([]byte, 16)
	if _, err := rand.Read(tokenRaw); err != nil {
		return nil, fmt.Errorf("audio: 生成代理 token: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("audio: 启动 header 代理: %w", err)
	}
	p := &headerProxy{
		ln:      ln,
		target:  target,
		headers: cloneHeaders(headers),
	}
	p.client = &http.Client{
		Transport: &http.Transport{
			Proxy:              nil,
			DisableCompression: true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("audio: 音频地址重定向过多")
			}
			// 只在原始 host 上带登录 Cookie；跨域重定向只带 UA/Referer。
			same := sameHost(req.URL.String(), p.target)
			if !same {
				req.Header.Del("Cookie")
				req.Header.Del("Authorization")
			}
			for k, v := range p.headers {
				if !same && (strings.EqualFold(k, "Cookie") || strings.EqualFold(k, "Authorization")) {
					continue
				}
				req.Header.Set(k, v)
			}
			return nil
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/"+hex.EncodeToString(tokenRaw), p.serve)
	p.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	p.url = "http://" + ln.Addr().String() + "/" + hex.EncodeToString(tokenRaw)
	go func() {
		_ = p.server.Serve(ln)
	}()
	return p, nil
}

func cloneHeaders(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		out[k] = v
	}
	return out
}

func sameHost(a string, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	return strings.EqualFold(ua.Host, ub.Host)
}

func (p *headerProxy) URL() string { return p.url }

func (p *headerProxy) Close() error {
	var err error
	p.once.Do(func() {
		err = p.server.Close()
	})
	return err
}

func (p *headerProxy) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, p.target, nil)
	if err != nil {
		http.Error(w, "bad target", http.StatusBadGateway)
		return
	}
	for _, k := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if v := r.Header.Get(k); v != "" {
			req.Header.Set(k, v)
		}
	}
	for k, v := range p.headers {
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		if r.Context().Err() == nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
		}
		return
	}
	defer resp.Body.Close()

	for _, k := range []string{
		"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges",
		"Last-Modified", "ETag", "Cache-Control",
	} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

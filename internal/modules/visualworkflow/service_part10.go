package visualworkflow

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func resolveURLMetadata(raw string) (map[string]any, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Hostname() == "" {
		return nil, fmt.Errorf("valid source_url is required")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("only http/https source_url is supported")
	}
	if isBlockedSourceHost(u.Hostname()) {
		return nil, fmt.Errorf("source_url host is not allowed for resolver")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", "V-Ecommerce-SourceResolver/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("source_url fetch failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("source_url returned unsupported status")
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	text := string(body)
	meta := map[string]any{"url": u.String(), "host": u.Hostname(), "status_code": resp.StatusCode, "content_type": resp.Header.Get("Content-Type")}
	if m := titleTagRe.FindStringSubmatch(text); len(m) == 2 {
		meta["title"] = strings.TrimSpace(htmlUnescapeLite(m[1]))
	}
	og := map[string]any{}
	for _, m := range metaTagRe.FindAllStringSubmatch(text, 32) {
		key := strings.ToLower(strings.TrimSpace(m[1]))
		val := strings.TrimSpace(htmlUnescapeLite(m[2]))
		if val == "" {
			continue
		}
		switch key {
		case "og:title", "twitter:title", "description", "og:description", "twitter:description", "og:image", "twitter:image", "og:type", "og:site_name":
			og[key] = val
		}
	}
	if len(og) > 0 {
		meta["open_graph"] = og
	}
	return sanitizeGenerationManifestValue(meta).(map[string]any), nil
}

func isBlockedSourceHost(host string) bool {
	if allowPrivateSourceResolverHosts {
		return false
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return true
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}

func htmlUnescapeLite(s string) string {
	repls := map[string]string{"&amp;": "&", "&quot;": "\"", "&#34;": "\"", "&#39;": "'", "&lt;": "<", "&gt;": ">", "\n": " ", "\t": " "}
	for old, newv := range repls {
		s = strings.ReplaceAll(s, old, newv)
	}
	return strings.TrimSpace(s)
}

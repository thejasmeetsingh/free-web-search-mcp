package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	readability "github.com/go-shiori/go-readability"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/thejasmeetsingh/free-web-search-mcp/pkg/services"
)

const (
	maxBodyBytes   = 5 << 20 // 5 MB cap on downloaded pages
	maxCachedChars = 200_000 // hard cap per cached page; also the effective ceiling for max_chars
)

type fetchArgs struct {
	URL      string `json:"url" jsonschema:"The http or https URL of the page to extract"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"Maximum characters of content to return (default 20000)"`
	NoCache  bool   `json:"no_cache,omitempty" jsonschema:"Skip the cache and fetch the page fresh"`
}

var FetchCache *services.Cache[string]

// No Proxy is set on purpose: the dialer check below inspects the real
// destination IP, which wouldn't work if connections went through a proxy.
var fetchClient = &http.Client{
	Timeout: 25 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			Control: blockPrivateIPs,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// blockPrivateIPs runs on every outgoing connection (including redirects)
// with the already-resolved IP, so DNS tricks can't sneak past it.
func blockPrivateIPs(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("invalid IP %q", host)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return fmt.Errorf("blocked non-public address %s", ip)
	}
	return nil
}

func WebFetch(ctx context.Context, _ *mcp.CallToolRequest, args fetchArgs) (*mcp.CallToolResult, any, error) {
	u, err := url.Parse(strings.TrimSpace(args.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, nil, errors.New("url must be a valid http or https URL")
	}
	u.Fragment = "" // #section anchors don't change the page

	maxChars := args.MaxChars
	if maxChars <= 0 {
		maxChars = 20000
	}

	key := u.String()
	if !args.NoCache {
		if text, ok := FetchCache.Get(key); ok {
			log.Printf("fetch cache hit: %s", key)
			return textResult(truncate(text, maxChars)), nil, nil
		}
	}

	text, err := fetchPage(ctx, u)
	if err != nil {
		return nil, nil, err
	}
	FetchCache.Set(key, text)

	return textResult(truncate(text, maxChars)), nil, nil
}

// fetchPage downloads a URL and returns its content as Markdown (or plain
// text), capped at maxCachedChars. This is the part that gets cached.
func fetchPage(ctx context.Context, u *url.URL) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := fetchClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	isHTML := contentType == "" || strings.Contains(contentType, "html")
	if !isHTML && !strings.HasPrefix(contentType, "text/") {
		return "", fmt.Errorf("unsupported content type %q", contentType)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	finalURL := resp.Request.URL // after redirects

	// Plain text and similar: return as-is.
	if !isHTML {
		return truncate(string(body), maxCachedChars), nil
	}

	var title, byline, markdown string

	article, rerr := readability.FromReader(bytes.NewReader(body), finalURL)
	if rerr == nil && strings.TrimSpace(article.Content) != "" {
		title, byline = article.Title, article.Byline
		markdown, _ = htmltomarkdown.ConvertString(article.Content)
	}

	// Fallback: readability found nothing useful, convert the whole page.
	if strings.TrimSpace(markdown) == "" {
		md, cerr := htmltomarkdown.ConvertString(string(body))
		if cerr != nil {
			return "", fmt.Errorf("failed to convert page to markdown: %w", cerr)
		}
		markdown = md
	}

	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, "# %s\n\n", title)
	}
	fmt.Fprintf(&b, "Source: %s\n", finalURL.String())
	if byline != "" {
		fmt.Fprintf(&b, "Author: %s\n", byline)
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(markdown))

	return truncate(b.String(), maxCachedChars), nil
}

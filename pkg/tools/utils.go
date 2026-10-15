package tools

import (
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: s}},
	}
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n\n[... content truncated ...]"
}

// unwrapDDG turns //duckduckgo.com/l/?uddg=<encoded> into the real target URL.
func unwrapDDG(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if strings.HasSuffix(u.Host, "duckduckgo.com") && u.Path == "/l/" {
		if target := u.Query().Get("uddg"); target != "" {
			return target
		}
	}
	return href
}

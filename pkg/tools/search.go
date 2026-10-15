package tools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/thejasmeetsingh/free-web-search-mcp/pkg/services"
)

const (
	userAgent        = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
	maxSearchResults = 20
)

type (
	searchArgs struct {
		Query      string `json:"query" jsonschema:"The search query"`
		MaxResults int    `json:"max_results,omitempty" jsonschema:"Maximum number of results to return (1-20, default 10)"`
		NoCache    bool   `json:"no_cache,omitempty" jsonschema:"Skip the cache and run a fresh search"`
	}
	SearchResult struct {
		Title   string
		URL     string
		Snippet string
	}
)

var (
	searchClient = &http.Client{Timeout: 15 * time.Second}
	SearchCache  *services.Cache[[]SearchResult]
)

func WebSearch(ctx context.Context, _ *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return nil, nil, errors.New("query is required")
	}

	limit := args.MaxResults
	if limit <= 0 {
		limit = 10
	}
	if limit > maxSearchResults {
		limit = maxSearchResults
	}

	// Normalize so "Go  MCP" and "go mcp" share one cache entry.
	key := strings.ToLower(strings.Join(strings.Fields(query), " "))

	var results []SearchResult
	hit := false
	if !args.NoCache {
		results, hit = SearchCache.Get(key)
	}

	if hit {
		log.Printf("search cache hit: %q", key)
	} else {
		var err error
		// Always fetch the maximum so one cache entry serves any max_results.
		results, err = duckDuckGoSearch(ctx, query, maxSearchResults)
		if err != nil {
			return nil, nil, err
		}
		if len(results) > 0 { // never cache empty results; they may be a temporary block
			SearchCache.Set(key, results)
		}
	}

	if len(results) == 0 {
		return textResult("No results found."), nil, nil
	}
	if len(results) > limit {
		results = results[:limit]
	}

	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Snippet)
	}
	return textResult(b.String()), nil, nil
}

func duckDuckGoSearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	form := url.Values{"q": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://html.duckduckgo.com/html/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := searchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// DDG answers 202 when it decides you look like a bot.
		return nil, fmt.Errorf("duckduckgo returned status %d (possibly rate-limited, try again later)", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse search results: %w", err)
	}

	var results []SearchResult
	doc.Find("div.result").Each(func(_ int, s *goquery.Selection) {
		if len(results) >= limit || s.HasClass("result--ad") {
			return
		}
		a := s.Find("a.result__a").First()
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		title := strings.TrimSpace(a.Text())
		link := unwrapDDG(href)
		if title == "" || link == "" {
			return
		}
		results = append(results, SearchResult{
			Title:   title,
			URL:     link,
			Snippet: strings.TrimSpace(s.Find(".result__snippet").First().Text()),
		})
	})
	return results, nil
}

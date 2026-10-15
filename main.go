package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/thejasmeetsingh/free-web-search-mcp/pkg/services"
	"github.com/thejasmeetsingh/free-web-search-mcp/pkg/tools"
)

func main() {
	transport := flag.String("transport", "stdio", "transport to use: stdio or http")
	addr := flag.String("addr", "127.0.0.1:8080", "listen address (http transport only)")
	searchTTL := flag.Duration("search-cache-ttl", 10*time.Minute, "how long search results are cached")
	fetchTTL := flag.Duration("fetch-cache-ttl", 30*time.Minute, "how long fetched pages are cached")
	cacheSize := flag.Int("cache-size", 100, "max entries per cache (0 disables caching)")
	flag.Parse()

	// stdout belongs to the MCP protocol in stdio mode, so log to stderr only.
	log.SetOutput(os.Stderr)

	// Retrieve the active version
	version, err := os.ReadFile("VERSION")
	if err != nil {
		log.Fatalf("server error: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "free-web-search-mcp",
		Version: strings.Trim(string(version), " "),
	}, nil)

	// Initialize tools cache
	tools.SearchCache = services.NewCache[[]tools.SearchResult](*searchTTL, *cacheSize)
	tools.FetchCache = services.NewCache[string](*fetchTTL, *cacheSize)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "web_search",
		Description: "Search the web (DuckDuckGo) and return a list of results with title, URL and snippet.",
	}, tools.WebSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "web_fetch",
		Description: "Fetch a web page by URL and return its main readable content as Markdown.",
	}, tools.WebFetch)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	switch *transport {
	case "stdio":
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatalf("server error: %v", err)
		}

	case "http":
		handler := mcp.NewStreamableHTTPHandler(
			func(*http.Request) *mcp.Server { return server }, nil,
		)
		mux := http.NewServeMux()
		mux.Handle("/mcp", handler)

		srv := &http.Server{Addr: *addr, Handler: mux}
		go func() {
			<-ctx.Done()
			_ = srv.Shutdown(context.Background())
		}()

		log.Printf("MCP server listening on http://%s/mcp", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}

	default:
		log.Fatalf("unknown transport %q (use stdio or http)", *transport)
	}
}

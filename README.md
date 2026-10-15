# Free Web Search MCP

A lightweight [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server written in Go that gives AI assistants two abilities, with **no API keys and no paid third-party services**:

- **`web_search`**: search the web and get back titles, URLs and snippets
- **`web_fetch`**: fetch a web page and get its main content back as clean Markdown

It ships as a single binary and supports both **stdio** and **Streamable HTTP** transports.

## Features

- No API keys, accounts or subscriptions
- Search via DuckDuckGo's HTML endpoint
- Readable-content extraction (strips navigation, ads and footers) converted to Markdown
- In-memory caching with TTL expiry and LRU eviction
- Works with any MCP client: Claude Code, Claude Desktop, Cursor, and others
- Built-in SSRF protection on page fetching
- Response size and output length limits
- Single static binary, easy to deploy

## Tools

### `web_search`

Searches the web and returns a numbered list of results.

| Parameter     | Type    | Required | Description                                       |
| ------------- | ------- | -------- | ------------------------------------------------- |
| `query`       | string  | yes      | The search query                                  |
| `max_results` | integer | no       | Number of results to return (1-20, default `10`)  |
| `no_cache`    | boolean | no       | Skip the cache and fetch fresh results            |

### `web_fetch`

Fetches a page and returns its main content as Markdown, prefixed with the title, source URL and author when available.

| Parameter   | Type    | Required | Description                                          |
| ----------- | ------- | -------- | ---------------------------------------------------- |
| `url`       | string  | yes      | The `http` or `https` URL to extract                 |
| `max_chars` | integer | no       | Maximum characters to return (default `20000`)       |
| `no_cache`  | boolean | no       | Skip the cache and fetch fresh results               |

Plain-text responses are returned as-is. Other content types (PDFs, images, etc.) are rejected.

## Installation

### macOS

```bash
brew tap thejasmeetsingh/free-web-search-mcp https://github.com/thejasmeetsingh/free-web-search-mcp
brew install --cask free-web-search-mcp
```

### Linux

```bash
wget https://github.com/thejasmeetsingh/free-web-search-mcp/releases/download/v{tag}/free-web-search-mcp_{tag}_linux_x86_64.deb
sudo dpkg -i free-web-search-mcp_{tag}_linux_x86_64.deb
```

> Replace `{tag}` with the specific version number from the [releases page](https://github.com/thejasmeetsingh/free-web-search-mcp/releases) (e.g. `0.1.0`).

### Windows

```bash
scoop bucket add thejasmeetsingh https://github.com/thejasmeetsingh/free-web-search-mcp
scoop install thejasmeetsingh/free-web-search-mcp
```

## Usage

### stdio (default)

The client launches the server as a subprocess. This is the simplest option for local use.

```bash
./free-web-search-mcp
```

### Streamable HTTP

Runs the server as a network service.

```bash
./free-web-search-mcp --transport http --addr 127.0.0.1:8080
```

The MCP endpoint is then available at `http://127.0.0.1:8080/mcp`.

| Flag                 | Default           | Description                                         |
| -------------------- | ----------------- | --------------------------------------------------- |
| `--transport`        | `stdio`           | Transport to use: `stdio` or `http`                 |
| `--addr`             | `127.0.0.1:8080`  | Listen address (`http` only)                        |
| `--search-cache-ttl` | `10m`             | How long search results are cached                  |
| `--fetch-cache-ttl`  | `30m`             | How long fetched pages are cached                   |
| `--cache-size`       | `100`             | Max entries per cache (`0` disables caching)        |

## Example prompts

- "Search the web for the latest Go release notes and summarize the highlights."
- "Find three articles about MCP servers, then fetch the most relevant one and give me the key points."
- "Fetch https://example.com/blog/post and turn it into bullet-point notes."

## How it works

**Search.** The server sends the query to DuckDuckGo's HTML endpoint, parses the result list with [goquery](https://github.com/PuerkitoBio/goquery), and unwraps DuckDuckGo's redirect links to return the real destination URLs.

**Fetch.** The server downloads the page (capped at 5 MB), extracts the main article content with [go-readability](https://github.com/go-shiori/go-readability), and converts it to Markdown with [html-to-markdown](https://github.com/JohannesKaufmann/html-to-markdown). If readability finds nothing, the whole page is converted instead.

## Caching

Search results and fetched pages are cached in memory to avoid repeat network
requests and to reduce the chance of being rate-limited.

- Search results are cached by normalized query (default TTL: 10 minutes).
- Fetched pages are cached by URL (default TTL: 30 minutes).
- Each cache holds up to `--cache-size` entries; the least recently used entry
  is evicted first. Set `--cache-size 0` to disable caching.
- Only successful responses are cached. Errors and empty search results are not.
- Pass `no_cache: true` to either tool to bypass the cache and refresh the entry.
- The cache lives in the process, so it is cleared on restart. With the stdio
  transport, each client session starts its own process and has its own cache.

Built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk).

## Security

`web_fetch` makes requests to URLs supplied by the model, which means untrusted input can influence where your machine connects. To limit the risk:

- **SSRF protection:** connections to loopback, private (RFC 1918), link-local, multicast and unspecified addresses are blocked. The check runs on the resolved IP for every connection, including redirects.
- **Limits:** only `http`/`https`, at most 5 redirects, a 5 MB body cap and request timeouts.
- **HTTP transport has no authentication.** It binds to `127.0.0.1` by default. If you expose it beyond localhost, put it behind a reverse proxy that handles authentication.
- Fetched page content is untrusted. A web page can contain text that tries to instruct the model (prompt injection). Treat tool output as data.

## Limitations

- **Search relies on scraping DuckDuckGo.** This is unofficial: heavy use may be rate-limited, and a change to DuckDuckGo's markup can break result parsing. The selectors live in `search.go`.
- **No JavaScript rendering.** Pages that build their content client-side may return little or no content.
- **No proxy support for fetching**, since the SSRF check needs to see the real destination address.
- Only text-based pages are supported (no PDFs or binary files).

## Troubleshooting

| Symptom                                           | Likely cause / fix                                                                        |
| ------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `duckduckgo returned status 202`                  | Rate-limited or bot-detected. Wait a while and retry, or reduce request frequency.        |
| Search returns "No results found."                | Query returned nothing, or DuckDuckGo's HTML structure changed.                           |
| `blocked non-public address ...`                  | The URL resolves to a private or local address, which is blocked by design.               |
| Fetched content is empty or very short            | The page likely renders its content with JavaScript.                                      |
| `unsupported content type`                        | The URL points to a PDF, image or other non-text resource.                                |
| Client can't connect over stdio                   | Use an absolute path to the binary and make sure it is executable.                        |
| Results or page content look outdated             | They may be served from cache. Use `no_cache`, lower the TTL flags, or restart the server.           |

## Contributing

Issues and pull requests are welcome. Ideas for improvement:

- Pluggable search backends (e.g. SearXNG)
- Optional headless-browser fallback for JavaScript-rendered pages
- PDF text extraction

## License

MIT License. See [LICENSE](LICENSE) for details.
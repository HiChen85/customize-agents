package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/HiChen85/customize-agents/llm"
)

// NewWebSearchTool creates a web search tool that uses a configurable search API.
// Supports Tavily-style API (common in AI agent frameworks).
func NewWebSearchTool(apiKey string, baseURL string) Tool {
	if baseURL == "" {
		baseURL = "https://api.tavily.com"
	}
	client := &http.Client{Timeout: 30 * time.Second}

	return Tool{
		Definition: llm.ToolDef{
			Name:        "web_search",
			Description: "Search the web for current information. Use this when you need up-to-date information that is not in your training data.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "The search query"
					},
					"max_results": {
						"type": "integer",
						"description": "Maximum number of results to return (default 5, max 10)"
					}
				},
				"required": ["query"]
			}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Query      string `json:"query"`
				MaxResults int    `json:"max_results"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			if params.MaxResults <= 0 {
				params.MaxResults = 5
			}
			if params.MaxResults > 10 {
				params.MaxResults = 10
			}

			reqBody, _ := json.Marshal(map[string]any{
				"api_key":              apiKey,
				"query":               params.Query,
				"max_results":         params.MaxResults,
				"include_answer":      true,
				"include_raw_content": false,
			})

			httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/search", strings.NewReader(string(reqBody)))
			if err != nil {
				return "", fmt.Errorf("create request: %w", err)
			}
			httpReq.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(httpReq)
			if err != nil {
				return "", fmt.Errorf("search request failed: %w", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return "", fmt.Errorf("read response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				return "", fmt.Errorf("search API error (status %d): %s", resp.StatusCode, string(body))
			}

			var result struct {
				Answer  string `json:"answer"`
				Results []struct {
					Title   string `json:"title"`
					URL     string `json:"url"`
					Content string `json:"content"`
				} `json:"results"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				return "", fmt.Errorf("parse search results: %w", err)
			}

			var sb strings.Builder
			if result.Answer != "" {
				sb.WriteString("## Summary\n")
				sb.WriteString(result.Answer)
				sb.WriteString("\n\n")
			}
			sb.WriteString("## Search Results\n")
			for i, r := range result.Results {
				sb.WriteString(fmt.Sprintf("\n### %d. %s\n", i+1, r.Title))
				sb.WriteString(fmt.Sprintf("URL: %s\n", r.URL))
				content := r.Content
				if len(content) > 500 {
					content = content[:500] + "..."
				}
				sb.WriteString(content + "\n")
			}
			return sb.String(), nil
		},
	}
}

// NewWebFetchTool creates a tool that fetches content from a URL.
// Similar to Claude Code's WebFetch tool.
func NewWebFetchTool() Tool {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	return Tool{
		Definition: llm.ToolDef{
			Name:        "web_fetch",
			Description: "Fetch the content of a web page by URL. Returns the text content of the page. Useful for reading documentation, API references, or any public web page.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"url": {
						"type": "string",
						"description": "The URL to fetch"
					},
					"max_length": {
						"type": "integer",
						"description": "Maximum content length to return in characters (default 10000)"
					}
				},
				"required": ["url"]
			}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				URL       string `json:"url"`
				MaxLength int    `json:"max_length"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			if params.MaxLength <= 0 {
				params.MaxLength = 10000
			}

			parsedURL, err := url.Parse(params.URL)
			if err != nil {
				return "", fmt.Errorf("invalid URL: %w", err)
			}
			if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
				return "", fmt.Errorf("only http and https URLs are supported")
			}

			httpReq, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
			if err != nil {
				return "", fmt.Errorf("create request: %w", err)
			}
			httpReq.Header.Set("User-Agent", "CustomizeAgent/1.0")
			httpReq.Header.Set("Accept", "text/html,text/plain,application/json")

			resp, err := client.Do(httpReq)
			if err != nil {
				return "", fmt.Errorf("fetch failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
			}

			body, err := io.ReadAll(io.LimitReader(resp.Body, int64(params.MaxLength*2)))
			if err != nil {
				return "", fmt.Errorf("read body: %w", err)
			}

			content := string(body)
			content = stripHTMLTags(content)

			if len(content) > params.MaxLength {
				content = content[:params.MaxLength] + "\n\n[Content truncated...]"
			}

			return fmt.Sprintf("URL: %s\nStatus: %d\nContent-Type: %s\n\n%s",
				params.URL, resp.StatusCode, resp.Header.Get("Content-Type"), content), nil
		},
	}
}

// NewWriteFileTool creates a tool that writes content to a file.
func NewWriteFileTool() Tool {
	return Tool{
		Definition: llm.ToolDef{
			Name:        "write_file",
			Description: "Write content to a file. Creates the file if it doesn't exist, or overwrites it if it does. Creates parent directories as needed.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Absolute path to the file to write"
					},
					"content": {
						"type": "string",
						"description": "The content to write to the file"
					}
				},
				"required": ["path", "content"]
			}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			if params.Path == "" {
				return "", fmt.Errorf("path is required and cannot be empty")
			}
			if !filepath.IsAbs(params.Path) {
				return "", fmt.Errorf("path must be absolute, got: %q", params.Path)
			}

			dir := filepath.Dir(params.Path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return "", fmt.Errorf("create directory %s: %w", dir, err)
			}

			if err := os.WriteFile(params.Path, []byte(params.Content), 0644); err != nil {
				return "", fmt.Errorf("write file %s: %w", params.Path, err)
			}

			info, _ := os.Stat(params.Path)
			return fmt.Sprintf("Successfully wrote %d bytes to %s", info.Size(), params.Path), nil
		},
	}
}

// NewListDirTool creates a tool that lists directory contents.
func NewListDirTool() Tool {
	return Tool{
		Definition: llm.ToolDef{
			Name:        "list_dir",
			Description: "List the contents of a directory. Shows file names, sizes, and whether each entry is a file or directory.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Absolute path to the directory to list"
					},
					"recursive": {
						"type": "boolean",
						"description": "Whether to list recursively (default false, max depth 3)"
					}
				},
				"required": ["path"]
			}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Path      string `json:"path"`
				Recursive bool   `json:"recursive"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}

			if params.Recursive {
				return listDirRecursive(params.Path, 3)
			}

			entries, err := os.ReadDir(params.Path)
			if err != nil {
				return "", fmt.Errorf("read directory: %w", err)
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Directory: %s\n\n", params.Path))
			for _, entry := range entries {
				info, _ := entry.Info()
				typeStr := "FILE"
				if entry.IsDir() {
					typeStr = "DIR "
				}
				size := int64(0)
				if info != nil {
					size = info.Size()
				}
				sb.WriteString(fmt.Sprintf("  [%s] %-40s %8d bytes\n", typeStr, entry.Name(), size))
			}
			sb.WriteString(fmt.Sprintf("\nTotal: %d entries", len(entries)))
			return sb.String(), nil
		},
	}
}

// NewGrepTool creates a tool that searches for patterns in files.
func NewGrepTool() Tool {
	return Tool{
		Definition: llm.ToolDef{
			Name:        "grep",
			Description: "Search for a pattern in files. Supports regex patterns. Searches recursively in the given directory.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pattern": {
						"type": "string",
						"description": "The regex pattern to search for"
					},
					"path": {
						"type": "string",
						"description": "Directory or file path to search in"
					},
					"include": {
						"type": "string",
						"description": "File extension filter, e.g. '.go' or '.ts' (optional)"
					},
					"max_results": {
						"type": "integer",
						"description": "Maximum number of matching lines to return (default 50)"
					}
				},
				"required": ["pattern", "path"]
			}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Pattern    string `json:"pattern"`
				Path       string `json:"path"`
				Include    string `json:"include"`
				MaxResults int    `json:"max_results"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			if params.MaxResults <= 0 {
				params.MaxResults = 50
			}

			re, err := regexp.Compile(params.Pattern)
			if err != nil {
				return "", fmt.Errorf("invalid regex pattern: %w", err)
			}

			type match struct {
				File string
				Line int
				Text string
			}
			var matches []match

			err = filepath.Walk(params.Path, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if info.IsDir() {
					if strings.HasPrefix(info.Name(), ".") && path != params.Path {
						return filepath.SkipDir
					}
					if info.Name() == "node_modules" || info.Name() == "vendor" {
						return filepath.SkipDir
					}
					return nil
				}

				if params.Include != "" && !strings.HasSuffix(path, params.Include) {
					return nil
				}

				if info.Size() > 1024*1024 {
					return nil
				}

				data, err := os.ReadFile(path)
				if err != nil {
					return nil
				}

				lines := strings.Split(string(data), "\n")
				for i, line := range lines {
					if len(matches) >= params.MaxResults {
						return fmt.Errorf("max results reached")
					}
					if re.MatchString(line) {
						matches = append(matches, match{
							File: path,
							Line: i + 1,
							Text: strings.TrimSpace(line),
						})
					}
				}
				return nil
			})

			if err != nil && err.Error() != "max results reached" {
				return "", fmt.Errorf("search failed: %w", err)
			}

			if len(matches) == 0 {
				return fmt.Sprintf("No matches found for pattern '%s' in %s", params.Pattern, params.Path), nil
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Found %d matches for '%s':\n\n", len(matches), params.Pattern))
			currentFile := ""
			for _, m := range matches {
				if m.File != currentFile {
					currentFile = m.File
					sb.WriteString(fmt.Sprintf("## %s\n", m.File))
				}
				text := m.Text
				if len(text) > 200 {
					text = text[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("  L%d: %s\n", m.Line, text))
			}
			return sb.String(), nil
		},
	}
}

func listDirRecursive(root string, maxDepth int) (string, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Directory tree: %s\n\n", root))
	count := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}

		depth := strings.Count(rel, string(filepath.Separator))
		if depth >= maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if strings.HasPrefix(info.Name(), ".") && info.IsDir() {
			return filepath.SkipDir
		}

		indent := strings.Repeat("  ", depth)
		if info.IsDir() {
			sb.WriteString(fmt.Sprintf("%s%s/\n", indent, info.Name()))
		} else {
			sb.WriteString(fmt.Sprintf("%s%s (%d bytes)\n", indent, info.Name(), info.Size()))
		}
		count++
		if count > 500 {
			return fmt.Errorf("too many entries")
		}
		return nil
	})

	if err != nil && err.Error() != "too many entries" {
		return "", err
	}
	sb.WriteString(fmt.Sprintf("\nTotal: %d entries listed", count))
	return sb.String(), nil
}

func stripHTMLTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	cleaned := re.ReplaceAllString(s, "")
	re2 := regexp.MustCompile(`\n{3,}`)
	cleaned = re2.ReplaceAllString(cleaned, "\n\n")
	return strings.TrimSpace(cleaned)
}

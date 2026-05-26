package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWebSearchTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)

		if reqBody["query"] != "Go programming language" {
			t.Errorf("unexpected query: %v", reqBody["query"])
		}

		resp := map[string]any{
			"answer": "Go is a statically typed programming language designed at Google.",
			"results": []map[string]any{
				{
					"title":   "The Go Programming Language",
					"url":     "https://go.dev",
					"content": "Go is an open source programming language supported by Google.",
				},
				{
					"title":   "Go Wikipedia",
					"url":     "https://en.wikipedia.org/wiki/Go_(programming_language)",
					"content": "Go is a statically typed, compiled high-level programming language.",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tool := NewWebSearchTool("test-api-key", server.URL)

	input, _ := json.Marshal(map[string]any{
		"query":       "Go programming language",
		"max_results": 5,
	})

	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !contains(result, "Go is a statically typed") {
		t.Errorf("expected answer in result, got: %s", result[:200])
	}
	if !contains(result, "https://go.dev") {
		t.Errorf("expected URL in result")
	}
}

func TestWebFetchTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello from test server!\nThis is a test page."))
	}))
	defer server.Close()

	tool := NewWebFetchTool()

	input, _ := json.Marshal(map[string]string{"url": server.URL})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "Hello from test server!") {
		t.Errorf("expected content in result, got: %s", result)
	}
	if !contains(result, "Status: 200") {
		t.Errorf("expected status in result")
	}
}

func TestWebFetchTool_InvalidScheme(t *testing.T) {
	tool := NewWebFetchTool()

	input, _ := json.Marshal(map[string]string{"url": "ftp://evil.com/file"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for non-http URL")
	}
}

func TestWriteFileTool(t *testing.T) {
	dir := t.TempDir()
	testPath := filepath.Join(dir, "subdir", "test.txt")

	tool := NewWriteFileTool()

	input, _ := json.Marshal(map[string]string{
		"path":    testPath,
		"content": "Hello, World!\nSecond line.",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "Successfully wrote") {
		t.Errorf("unexpected result: %s", result)
	}

	data, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != "Hello, World!\nSecond line." {
		t.Errorf("unexpected file content: %s", string(data))
	}
}

func TestListDirTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file1.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("hello"), 0644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)

	tool := NewListDirTool()

	input, _ := json.Marshal(map[string]any{"path": dir, "recursive": false})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "file1.go") {
		t.Errorf("expected file1.go in result")
	}
	if !contains(result, "subdir") {
		t.Errorf("expected subdir in result")
	}
	if !contains(result, "Total: 3 entries") {
		t.Errorf("expected 3 entries, got: %s", result)
	}
}

func TestListDirTool_Recursive(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "root.go"), []byte("package main"), 0644)
	os.MkdirAll(filepath.Join(dir, "pkg", "inner"), 0755)
	os.WriteFile(filepath.Join(dir, "pkg", "lib.go"), []byte("package pkg"), 0644)
	os.WriteFile(filepath.Join(dir, "pkg", "inner", "deep.go"), []byte("package inner"), 0644)

	tool := NewListDirTool()

	input, _ := json.Marshal(map[string]any{"path": dir, "recursive": true})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "root.go") {
		t.Errorf("expected root.go in result")
	}
	if !contains(result, "lib.go") {
		t.Errorf("expected lib.go in result")
	}
	if !contains(result, "deep.go") {
		t.Errorf("expected deep.go in recursive result")
	}
}

func TestGrepTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "lib.go"), []byte("package main\n\nfunc helper() string {\n\treturn \"world\"\n}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello\nThis is a readme.\n"), 0644)

	tool := NewGrepTool()

	t.Run("basic search", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"pattern": "func",
			"path":    dir,
		})
		result, err := tool.Execute(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !contains(result, "func main()") {
			t.Errorf("expected 'func main()' in result, got: %s", result)
		}
		if !contains(result, "func helper()") {
			t.Errorf("expected 'func helper()' in result")
		}
	})

	t.Run("with file filter", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"pattern": "hello",
			"path":    dir,
			"include": ".go",
		})
		result, err := tool.Execute(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !contains(result, "hello") {
			t.Errorf("expected 'hello' match in .go files")
		}
		if contains(result, "readme.md") {
			t.Errorf("should not match .md file when filtering .go")
		}
	})

	t.Run("regex pattern", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"pattern": `func \w+\(\)`,
			"path":    dir,
		})
		result, err := tool.Execute(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !contains(result, "Found") {
			t.Errorf("expected matches, got: %s", result)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"pattern": "nonexistentpattern12345",
			"path":    dir,
		})
		result, err := tool.Execute(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !contains(result, "No matches found") {
			t.Errorf("expected 'No matches found', got: %s", result)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

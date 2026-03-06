// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package handlers

import (
	"strings"
	"testing"

	"yaaicms/internal/engine"
)

func TestParseNumberedList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "dot numbered",
			input:    "1. First Title\n2. Second Title\n3. Third Title",
			expected: []string{"First Title", "Second Title", "Third Title"},
		},
		{
			name:     "paren numbered",
			input:    "1) First Title\n2) Second Title",
			expected: []string{"First Title", "Second Title"},
		},
		{
			name:     "dash bullets",
			input:    "- First Title\n- Second Title",
			expected: []string{"First Title", "Second Title"},
		},
		{
			name:     "with quotes",
			input:    `1. "First Title"` + "\n" + `2. 'Second Title'`,
			expected: []string{"First Title", "Second Title"},
		},
		{
			name:     "with empty lines",
			input:    "\n1. First\n\n2. Second\n\n",
			expected: []string{"First", "Second"},
		},
		{
			name:     "no prefix",
			input:    "Just a single line",
			expected: []string{"Just a single line"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseNumberedList(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("got %d items, want %d: %v", len(result), len(tt.expected), result)
			}
			for i, item := range result {
				if item != tt.expected[i] {
					t.Errorf("item %d: got %q, want %q", i, item, tt.expected[i])
				}
			}
		})
	}
}

func TestParseSEOResult(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantDesc     string
		wantKeywords string
	}{
		{
			name:         "standard format",
			input:        "DESCRIPTION: A great article about Go.\nKEYWORDS: go, programming, google",
			wantDesc:     "A great article about Go.",
			wantKeywords: "go, programming, google",
		},
		{
			name:         "lowercase prefixes",
			input:        "description: Some description.\nkeywords: key1, key2",
			wantDesc:     "Some description.",
			wantKeywords: "key1, key2",
		},
		{
			name:         "meta prefixes",
			input:        "Meta Description: SEO optimized.\nMeta Keywords: seo, web",
			wantDesc:     "SEO optimized.",
			wantKeywords: "seo, web",
		},
		{
			name:         "extra whitespace",
			input:        "\n\nDESCRIPTION:   Spaced out.  \nKEYWORDS:   a, b, c  \n",
			wantDesc:     "Spaced out.",
			wantKeywords: "a, b, c",
		},
		{
			name:         "no match",
			input:        "Some random text without structure",
			wantDesc:     "",
			wantKeywords: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc, kw := parseSEOResult(tt.input)
			if desc != tt.wantDesc {
				t.Errorf("description: got %q, want %q", desc, tt.wantDesc)
			}
			if kw != tt.wantKeywords {
				t.Errorf("keywords: got %q, want %q", kw, tt.wantKeywords)
			}
		})
	}
}

func TestParseTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "comma separated",
			input:    "go, programming, web development, api",
			expected: []string{"go", "programming", "web development", "api"},
		},
		{
			name:     "with quotes",
			input:    `"go", "programming", "web"`,
			expected: []string{"go", "programming", "web"},
		},
		{
			name:     "with dashes and bullets",
			input:    "- go, - programming, * web",
			expected: []string{"go", "programming", "web"},
		},
		{
			name:     "extra whitespace",
			input:    "  go ,  programming  , web  ",
			expected: []string{"go", "programming", "web"},
		},
		{
			name:     "empty items filtered",
			input:    "go,,, programming",
			expected: []string{"go", "programming"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTags(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("got %d tags, want %d: %v", len(result), len(tt.expected), result)
			}
			for i, tag := range result {
				if tag != tt.expected[i] {
					t.Errorf("tag %d: got %q, want %q", i, tag, tt.expected[i])
				}
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 100); got != "short" {
		t.Errorf("short string: got %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncated: got %q, want %q", got, "hello...")
	}
}

func TestExtractHTMLFromResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain html",
			input: "<div>Hello</div>",
			want:  "<div>Hello</div>",
		},
		{
			name:  "html code fence",
			input: "```html\n<div>Hello</div>\n```",
			want:  "<div>Hello</div>",
		},
		{
			name:  "generic code fence",
			input: "```\n<div>Hello</div>\n```",
			want:  "<div>Hello</div>",
		},
		{
			name:  "with surrounding whitespace",
			input: "\n\n<div>Hello</div>\n\n",
			want:  "<div>Hello</div>",
		},
		{
			name:  "code fence with extra text after closing",
			input: "```html\n<header>Nav</header>\n```\n",
			want:  "<header>Nav</header>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHTMLFromResponse(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildTemplateSystemPrompt(t *testing.T) {
	// Verify each template type produces a prompt containing the right variables.
	tests := []struct {
		tmplType string
		contains []string
	}{
		{"header", []string{"SiteTitle", "Slogan", "Year", "TEMPLATE TYPE: Header"}},
		{"footer", []string{"SiteTitle", "Slogan", "Year", "TEMPLATE TYPE: Footer"}},
		{"page", []string{"Title", "Body", "Header", "Footer", "MetaDescription", "TEMPLATE TYPE: Page"}},
		{"article_loop", []string{"range .Posts", "Title", "Slug", "Excerpt", "TEMPLATE TYPE: Article Loop"}},
	}

	for _, tt := range tests {
		t.Run(tt.tmplType, func(t *testing.T) {
			prompt := buildTemplateSystemPrompt(tt.tmplType)
			for _, s := range tt.contains {
				if !containsStr(prompt, s) {
					t.Errorf("prompt for %q should contain %q", tt.tmplType, s)
				}
			}
		})
	}
}

func containsStr(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 && strings.Contains(haystack, needle)
}

func TestBuildPreviewData(t *testing.T) {
	// Page preview should return PageData.
	pageData := buildPreviewData("page")
	if pd, ok := pageData.(engine.PageData); ok {
		if pd.SiteTitle != "YaaiCMS" {
			t.Errorf("page SiteTitle: got %q", pd.SiteTitle)
		}
		if pd.Title == "" {
			t.Error("page Title should not be empty")
		}
	} else {
		t.Errorf("page preview should return engine.PageData, got %T", pageData)
	}

	// Article loop preview should return ListData with posts.
	listData := buildPreviewData("article_loop")
	if ld, ok := listData.(engine.ListData); ok {
		if len(ld.Posts) == 0 {
			t.Error("article_loop should have sample posts")
		}
	} else {
		t.Errorf("article_loop preview should return engine.ListData, got %T", listData)
	}

	// Header/footer should return a struct with SiteTitle, Slogan, and Year.
	headerData := buildPreviewData("header")
	if headerData == nil {
		t.Error("header preview should not be nil")
	}
}

func TestQuoteJSString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "hello", "'hello'"},
		{"single quote", "it's", `'it\'s'`},
		{"double quote", `he said "hi"`, `'he said \x22hi\x22'`},
		{"html tags", "<script>alert(1)</script>", `'\x3cscript\x3ealert(1)\x3c/script\x3e'`},
		{"newline", "line1\nline2", `'line1\nline2'`},
		{"backslash", `back\slash`, `'back\\slash'`},
		{"ampersand", "a&b", `'a\x26b'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteJSString(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

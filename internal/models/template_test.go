// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package models

import "testing"

// TestTemplateTypeConstants verifies that template type string constants have
// the expected values.
func TestTemplateTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		tt       TemplateType
		expected string
	}{
		{name: "header", tt: TemplateTypeHeader, expected: "header"},
		{name: "footer", tt: TemplateTypeFooter, expected: "footer"},
		{name: "page", tt: TemplateTypePage, expected: "page"},
		{name: "article_loop", tt: TemplateTypeArticleLoop, expected: "article_loop"},
		{name: "author_page", tt: TemplateTypeAuthorPage, expected: "author_page"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.tt) != tc.expected {
				t.Errorf("TemplateType %s = %q, want %q", tc.name, string(tc.tt), tc.expected)
			}
		})
	}
}

// TestTemplateTypeDistinct ensures all template type constants are unique.
func TestTemplateTypeDistinct(t *testing.T) {
	types := []TemplateType{
		TemplateTypeHeader,
		TemplateTypeFooter,
		TemplateTypePage,
		TemplateTypeArticleLoop,
		TemplateTypeAuthorPage,
	}

	seen := make(map[TemplateType]bool)
	for _, tt := range types {
		if seen[tt] {
			t.Errorf("duplicate TemplateType value: %q", tt)
		}
		seen[tt] = true
	}
}

// TestTemplateTypeNonEmpty ensures no template type constant is an empty string.
func TestTemplateTypeNonEmpty(t *testing.T) {
	types := []struct {
		name string
		tt   TemplateType
	}{
		{name: "Header", tt: TemplateTypeHeader},
		{name: "Footer", tt: TemplateTypeFooter},
		{name: "Page", tt: TemplateTypePage},
		{name: "ArticleLoop", tt: TemplateTypeArticleLoop},
		{name: "AuthorPage", tt: TemplateTypeAuthorPage},
	}

	for _, tc := range types {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.tt) == "" {
				t.Errorf("TemplateType%s must not be empty", tc.name)
			}
		})
	}
}

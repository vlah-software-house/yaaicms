// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package markdown converts Markdown source text into sanitized HTML using
// goldmark for rendering and bluemonday for XSS protection.
package markdown

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
)

// md is the configured goldmark instance, reused across calls.
var md = goldmark.New( //nolint:gochecknoglobals // singleton markdown renderer, safe for concurrent use
	goldmark.WithExtensions(
		extension.GFM,            // GitHub-Flavored Markdown: tables, strikethrough, autolinks, task lists
		extension.Typographer,    // Smart quotes and dashes
		highlighting.NewHighlighting( // Syntax highlighting for fenced code blocks
			highlighting.WithStyle("monokai"),
			highlighting.WithFormatOptions(),
		),
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(), // Auto-generate heading IDs for anchors
	),
	goldmark.WithRendererOptions(
		html.WithUnsafe(), // Allow raw HTML blocks — sanitized by bluemonday before output
	),
)

// sanitizer strips dangerous HTML (scripts, iframes, event handlers) while
// preserving safe formatting tags used in content.
var sanitizer = buildSanitizer() //nolint:gochecknoglobals // singleton policy, safe for concurrent use

func buildSanitizer() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()

	// Allow class attributes on code/pre/span for syntax highlighting.
	p.AllowAttrs("class").Matching(bluemonday.SpaceSeparatedTokens).OnElements("code", "pre", "span", "div")

	// Allow id attributes on headings for anchor links.
	p.AllowAttrs("id").Matching(bluemonday.Paragraph).OnElements("h1", "h2", "h3", "h4", "h5", "h6")

	// Allow style attribute on common elements for inline formatting.
	p.AllowAttrs("style").OnElements("span", "div", "p", "td", "th", "table")

	// Allow data attributes for interactive components.
	p.AllowDataAttributes()

	// Allow video/audio embeds with safe attributes.
	p.AllowElements("video", "audio", "source", "figure", "figcaption")
	p.AllowAttrs("src", "type", "controls", "width", "height").OnElements("video", "audio", "source")

	return p
}

// ToHTML converts Markdown source into sanitized HTML. Raw HTML embedded in
// the Markdown is passed through goldmark (WithUnsafe) for backward
// compatibility, then sanitized by bluemonday to strip XSS vectors.
func ToHTML(source string) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return "", err
	}
	return sanitizer.Sanitize(buf.String()), nil
}

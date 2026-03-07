// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package imaging provides responsive image variant generation using libvips.
// It converts uploaded images into multiple WebP variants optimised for
// mobile, tablet, and desktop breakpoints. Variants smaller than the
// source image are generated; larger ones are skipped to avoid upscaling.
package imaging

import (
	"fmt"
	"log/slog"

	"github.com/davidbyttow/govips/v2/vips"
)

// Variant describes a single responsive image size.
type Variant struct {
	Name    string // e.g., "thumb", "sm", "md", "lg"
	Width   int    // Target width in pixels
	Quality int    // WebP quality 1-100
}

// DefaultVariants defines the standard breakpoints for responsive web images.
var DefaultVariants = []Variant{ //nolint:gochecknoglobals // constant configuration
	{Name: "thumb", Width: 320, Quality: 75},
	{Name: "sm", Width: 640, Quality: 80},
	{Name: "md", Width: 1024, Quality: 80},
	{Name: "lg", Width: 1920, Quality: 80},
}

// ProcessedImage holds one generated variant ready for upload.
type ProcessedImage struct {
	Name        string // Variant name (e.g., "sm")
	Width       int    // Actual output width
	Height      int    // Actual output height
	Data        []byte // WebP-encoded image bytes
	ContentType string // Always "image/webp"
}

// Startup initialises the libvips library. Call once at application start.
// concurrency controls the number of libvips worker threads (0 = auto).
func Startup(concurrency int) {
	cfg := &vips.Config{
		ConcurrencyLevel: concurrency,
		MaxCacheSize:     100,
		MaxCacheMem:      50 * 1024 * 1024, // 50 MB
	}
	vips.LoggingSettings(nil, vips.LogLevelWarning)
	vips.Startup(cfg)
	slog.Info("libvips started", "version", vips.Version)
}

// Shutdown releases libvips resources. Call at application shutdown.
func Shutdown() {
	vips.Shutdown()
}

// GenerateVariants creates WebP variants of the source image for each
// configured breakpoint. It skips variants wider than the original to
// avoid upscaling. Returns at least one variant (the smallest that fits).
func GenerateVariants(original []byte, variants []Variant) ([]ProcessedImage, error) {
	if len(variants) == 0 {
		variants = DefaultVariants
	}

	// Probe original dimensions without fully decoding.
	probe, err := vips.NewImageFromBuffer(original)
	if err != nil {
		return nil, fmt.Errorf("imaging: probe failed: %w", err)
	}
	origWidth := probe.Width()
	probe.Close()

	var results []ProcessedImage

	for _, v := range variants {
		targetWidth := v.Width

		// Cap at original width to avoid upscaling.
		if origWidth <= targetWidth {
			targetWidth = origWidth
		}

		// Height 0 triggers GLib warnings in some libvips versions; use a
		// very large value so only width constrains the resize.
		img, err := vips.NewThumbnailFromBuffer(original, targetWidth, targetWidth*10, vips.InterestingNone)
		if err != nil {
			return nil, fmt.Errorf("imaging: thumbnail %s (%dpx): %w", v.Name, targetWidth, err)
		}

		// Auto-rotate based on EXIF orientation, then strip metadata.
		if err := img.AutoRotate(); err != nil {
			img.Close()
			return nil, fmt.Errorf("imaging: autorotate %s: %w", v.Name, err)
		}

		params := vips.NewWebpExportParams()
		params.Quality = v.Quality
		params.Lossless = false
		params.StripMetadata = true

		buf, meta, err := img.ExportWebp(params)
		img.Close()
		if err != nil {
			return nil, fmt.Errorf("imaging: export %s: %w", v.Name, err)
		}

		results = append(results, ProcessedImage{
			Name:        v.Name,
			Width:       meta.Width,
			Height:      meta.Height,
			Data:        buf,
			ContentType: "image/webp",
		})

		// If we already processed the original-width image, no point
		// generating larger variants.
		if origWidth <= v.Width {
			break
		}
	}

	return results, nil
}

// avatarSize is the square dimension for processed avatar images (512px
// covers 3× Retina at typical 160px CSS display sizes).
const avatarSize = 512

// ProcessAvatar crops and resizes an image to a 512×512 square WebP
// suitable for user avatars. Uses attention-based smart cropping to
// keep faces and salient features centred.
func ProcessAvatar(original []byte) ([]byte, error) {
	img, err := vips.NewThumbnailFromBuffer(original, avatarSize, avatarSize, vips.InterestingAttention)
	if err != nil {
		return nil, fmt.Errorf("imaging: avatar thumbnail: %w", err)
	}
	defer img.Close()

	err = img.AutoRotate()
	if err != nil {
		return nil, fmt.Errorf("imaging: avatar autorotate: %w", err)
	}

	params := vips.NewWebpExportParams()
	params.Quality = 80
	params.Lossless = false
	params.StripMetadata = true

	buf, _, err := img.ExportWebp(params)
	if err != nil {
		return nil, fmt.Errorf("imaging: avatar export: %w", err)
	}
	return buf, nil
}

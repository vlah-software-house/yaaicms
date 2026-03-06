// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"

	"yaaicms/internal/imaging"
	"yaaicms/internal/middleware"
	"yaaicms/internal/models"
	"yaaicms/internal/render"
)

const (
	// maxUploadSize is the maximum allowed file upload size (50 MB).
	maxUploadSize = 50 << 20

	// presignExpiry is how long a presigned URL for private files is valid.
	presignExpiry = 1 * time.Hour
)

// allowedMediaTypes defines MIME types accepted for upload.
var allowedMediaTypes = map[string]bool{ //nolint:gochecknoglobals // constant lookup table
	"image/jpeg":      true,
	"image/png":       true,
	"image/gif":       true,
	"image/webp":      true,
	"image/svg+xml":   true,
	"application/pdf": true,
}

// variantTypes are image types that support responsive variant generation
// via libvips. GIF is excluded to preserve animation; SVG is vector.
var variantTypes = map[string]bool{ //nolint:gochecknoglobals // constant lookup table
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// MediaLibrary renders the media library admin page.
func (a *Admin) MediaLibrary(w http.ResponseWriter, r *http.Request) {
	if a.storageClient == nil {
		a.renderer.Page(w, r, "media_library", &render.PageData{
			Title:   "Media Library",
			Section: "media",
			Data:    map[string]any{"NoStorage": true},
		})
		return
	}

	sess := middleware.SessionFromCtx(r.Context())

	page := 0
	items, _ := a.mediaStore.List(sess.TenantID, 50, page*50)

	// Build URLs for each media item.
	type mediaView struct {
		models.Media
		URL      string
		ThumbURL string
	}
	var views []mediaView
	for _, m := range items {
		mv := mediaView{Media: m}
		if m.Bucket == a.storageClient.PublicBucket() {
			mv.URL = a.storageClient.FileURL(m.S3Key)
			if m.ThumbS3Key != nil {
				mv.ThumbURL = a.storageClient.FileURL(*m.ThumbS3Key)
			}
		}
		views = append(views, mv)
	}

	a.renderer.Page(w, r, "media_library", &render.PageData{
		Title:   "Media Library",
		Section: "media",
		Data: map[string]any{
			"Items":     views,
			"NoStorage": false,
		},
	})
}

// MediaListJSON returns media items as JSON for the in-editor media picker.
func (a *Admin) MediaListJSON(w http.ResponseWriter, r *http.Request) {
	if a.storageClient == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		return
	}

	sess := middleware.SessionFromCtx(r.Context())

	items, _ := a.mediaStore.List(sess.TenantID, 100, 0)

	type item struct {
		ID       string `json:"id"`
		URL      string `json:"url"`
		ThumbURL string `json:"thumb_url"`
		Filename string `json:"filename"`
		Type     string `json:"type"`
		AltText  string `json:"alt_text"`
	}

	var result []item
	for _, m := range items {
		if m.Bucket != a.storageClient.PublicBucket() {
			continue // Only show public-bucket images in the picker.
		}
		if !strings.HasPrefix(m.ContentType, "image/") {
			continue // Only images for the body picker.
		}
		it := item{
			ID:       m.ID.String(),
			URL:      a.storageClient.FileURL(m.S3Key),
			Filename: m.OriginalName,
			Type:     m.ContentType,
		}
		if m.ThumbS3Key != nil {
			it.ThumbURL = a.storageClient.FileURL(*m.ThumbS3Key)
		}
		if m.AltText != nil {
			it.AltText = *m.AltText
		}
		result = append(result, it)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": result})
}

// MediaUpload handles multipart file upload to S3.
func (a *Admin) MediaUpload(w http.ResponseWriter, r *http.Request) {
	if a.storageClient == nil {
		writeMediaError(w, "Object storage is not configured.", http.StatusServiceUnavailable)
		return
	}

	sess := middleware.SessionFromCtx(r.Context())

	// Limit request body to maxUploadSize + some overhead for form fields.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+1024)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeMediaError(w, "File too large. Maximum size is 50 MB.", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeMediaError(w, "No file provided.", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	// Validate file size.
	if header.Size > maxUploadSize {
		writeMediaError(w, "File too large. Maximum size is 50 MB.", http.StatusRequestEntityTooLarge)
		return
	}

	// Detect content type by sniffing the first 512 bytes.
	sniffBuf := make([]byte, 512)
	n, err := file.Read(sniffBuf)
	if err != nil && err != io.EOF {
		writeMediaError(w, "Failed to read file.", http.StatusInternalServerError)
		return
	}
	contentType := http.DetectContentType(sniffBuf[:n])

	// SVG detection: DetectContentType returns text/xml or application/xml for SVGs.
	if strings.HasSuffix(strings.ToLower(header.Filename), ".svg") &&
		(strings.Contains(contentType, "xml") || strings.Contains(contentType, "text/plain")) {
		contentType = "image/svg+xml"
	}

	if !allowedMediaTypes[contentType] {
		writeMediaError(w, fmt.Sprintf("File type %q is not allowed.", contentType), http.StatusBadRequest)
		return
	}

	// Seek back to start after sniffing.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeMediaError(w, "Failed to process file.", http.StatusInternalServerError)
		return
	}

	// Determine target bucket.
	bucket := a.storageClient.PublicBucket()
	if r.FormValue("bucket") == "private" {
		bucket = a.storageClient.PrivateBucket()
	}

	// Generate a unique storage key.
	now := time.Now()
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = extensionFromType(contentType)
	}
	fileID := uuid.New().String()
	s3Key := fmt.Sprintf("media/%d/%02d/%s%s", now.Year(), now.Month(), fileID, ext)

	// Read the entire file into memory for upload and thumbnail generation.
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		writeMediaError(w, "Failed to read file.", http.StatusInternalServerError)
		return
	}

	// Sanitize SVG uploads to prevent stored XSS (embedded scripts, event handlers).
	if contentType == "image/svg+xml" {
		fileBytes = sanitizeSVG(fileBytes)
	}

	// Upload original to S3.
	ctx := r.Context()
	if err := a.storageClient.Upload(ctx, bucket, s3Key, contentType, bytes.NewReader(fileBytes), int64(len(fileBytes))); err != nil {
		slog.Error("s3 upload failed", "error", err, "key", s3Key)
		writeMediaError(w, "Failed to upload file.", http.StatusInternalServerError)
		return
	}

	// Generate responsive WebP variants for supported image types.
	var thumbKey *string
	var pendingVariants []models.MediaVariant
	if variantTypes[contentType] {
		pendingVariants, thumbKey = a.generateAndUploadVariants(ctx, fileBytes, bucket, fileID, now)
	}

	// Store metadata in PostgreSQL.
	altText := r.FormValue("alt_text")
	media := &models.Media{
		Filename:     fileID + ext,
		OriginalName: header.Filename,
		ContentType:  contentType,
		SizeBytes:    int64(len(fileBytes)),
		Bucket:       bucket,
		S3Key:        s3Key,
		ThumbS3Key:   thumbKey,
		UploaderID:   sess.UserID,
	}
	if altText != "" {
		media.AltText = &altText
	}

	created, err := a.mediaStore.Create(sess.TenantID, media)
	if err != nil {
		slog.Error("media db insert failed", "error", err, "key", s3Key)
		writeMediaError(w, "Failed to save file metadata.", http.StatusInternalServerError)
		return
	}

	// Store variant records now that we have the media ID.
	a.saveVariants(created.ID, pendingVariants)

	// Build response URL.
	url := a.storageClient.FileURL(created.S3Key)
	var thumbURL string
	if created.ThumbS3Key != nil {
		thumbURL = a.storageClient.FileURL(*created.ThumbS3Key)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":        created.ID,
		"url":       url,
		"thumb_url": thumbURL,
		"filename":  created.OriginalName,
		"size":      created.HumanSize(),
		"type":      created.ContentType,
	})
}

// MediaDelete removes a media item from both S3 and the database.
func (a *Admin) MediaDelete(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Fetch variant S3 keys before deletion (CASCADE will remove them from DB).
	var variantKeys []string
	if a.variantStore != nil {
		variants, _ := a.variantStore.FindByMediaID(id)
		for _, v := range variants {
			variantKeys = append(variantKeys, v.S3Key)
		}
	}

	// Delete from DB first (returns the row for S3 cleanup).
	deleted, err := a.mediaStore.Delete(sess.TenantID, id)
	if err != nil {
		slog.Error("media db delete failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if deleted == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Clean up S3 objects (best-effort, don't fail the request).
	ctx := r.Context()
	if err := a.storageClient.Delete(ctx, deleted.Bucket, deleted.S3Key); err != nil {
		slog.Warn("s3 original delete failed", "error", err, "key", deleted.S3Key)
	}
	if deleted.ThumbS3Key != nil {
		if err := a.storageClient.Delete(ctx, deleted.Bucket, *deleted.ThumbS3Key); err != nil {
			slog.Warn("s3 thumbnail delete failed", "error", err, "key", *deleted.ThumbS3Key)
		}
	}
	for _, vk := range variantKeys {
		if err := a.storageClient.Delete(ctx, deleted.Bucket, vk); err != nil {
			slog.Warn("s3 variant delete failed", "error", err, "key", vk)
		}
	}

	// Return empty body for HTMX swap (removes the media card).
	w.WriteHeader(http.StatusOK)
}

// MediaServe provides the URL for a media item. Public files redirect to
// the direct S3 URL; private files get a time-limited presigned URL.
func (a *Admin) MediaServe(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	media, err := a.mediaStore.FindByID(sess.TenantID, id)
	if err != nil {
		slog.Error("media lookup failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if media == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	if media.Bucket == a.storageClient.PublicBucket() {
		http.Redirect(w, r, a.storageClient.FileURL(media.S3Key), http.StatusFound)
		return
	}

	// Private file — generate presigned URL.
	presigned, err := a.storageClient.PresignedURL(r.Context(), media.Bucket, media.S3Key, presignExpiry)
	if err != nil {
		slog.Error("presign failed", "error", err, "key", media.S3Key)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, presigned, http.StatusFound)
}

// generateAndUploadVariants creates responsive WebP variants for an image
// using libvips and uploads each variant to S3. Returns the variant metadata
// (without MediaID set — caller sets it after creating the media record) and
// the "thumb" variant's S3 key for the media thumbnail.
func (a *Admin) generateAndUploadVariants(ctx context.Context, fileBytes []byte, bucket, fileID string, now time.Time) ([]models.MediaVariant, *string) {
	variants, err := imaging.GenerateVariants(fileBytes, nil)
	if err != nil {
		slog.Warn("variant generation failed", "error", err)
		return nil, nil
	}

	var result []models.MediaVariant
	var thumbKey *string
	for _, v := range variants {
		vKey := fmt.Sprintf("media/%d/%02d/%s_%s.webp", now.Year(), now.Month(), fileID, v.Name)
		if err := a.storageClient.Upload(ctx, bucket, vKey, v.ContentType, bytes.NewReader(v.Data), int64(len(v.Data))); err != nil {
			slog.Warn("variant upload failed", "error", err, "key", vKey, "variant", v.Name)
			continue
		}
		result = append(result, models.MediaVariant{
			Name:        v.Name,
			Width:       v.Width,
			Height:      v.Height,
			S3Key:       vKey,
			ContentType: v.ContentType,
			SizeBytes:   int64(len(v.Data)),
		})
		if v.Name == "thumb" {
			tk := vKey
			thumbKey = &tk
		}
	}
	return result, thumbKey
}

// saveVariants persists variant metadata to the database, setting the MediaID
// on each variant. No-op if there are no variants or the store is nil.
func (a *Admin) saveVariants(mediaID uuid.UUID, variants []models.MediaVariant) {
	if len(variants) == 0 || a.variantStore == nil {
		return
	}
	for i := range variants {
		variants[i].MediaID = mediaID
	}
	if err := a.variantStore.CreateBatch(variants); err != nil {
		slog.Warn("variant metadata insert failed", "error", err, "media_id", mediaID)
	}
}

// MediaRegenerateVariants regenerates responsive WebP variants for a single
// media item. Deletes any existing variants first, then creates new ones from
// the original. Used from the admin media library's per-image action.
func (a *Admin) MediaRegenerateVariants(w http.ResponseWriter, r *http.Request) {
	if a.storageClient == nil || a.variantStore == nil {
		http.Error(w, "Storage not configured", http.StatusServiceUnavailable)
		return
	}

	sess := middleware.SessionFromCtx(r.Context())

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	media, err := a.mediaStore.FindByID(sess.TenantID, id)
	if err != nil || media == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	if !variantTypes[media.ContentType] {
		writeMediaError(w, "This media type does not support variants.", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Download the original image from S3.
	original, err := a.storageClient.Download(ctx, media.Bucket, media.S3Key)
	if err != nil {
		slog.Error("download original for regen failed", "error", err, "key", media.S3Key)
		writeMediaError(w, "Failed to download original image.", http.StatusInternalServerError)
		return
	}

	// Delete existing variants from S3 and DB.
	oldVariants, err := a.variantStore.DeleteByMediaID(id)
	if err != nil {
		slog.Warn("delete old variants failed", "error", err)
	}
	for _, v := range oldVariants {
		if err := a.storageClient.Delete(ctx, media.Bucket, v.S3Key); err != nil {
			slog.Warn("old variant s3 delete failed", "error", err, "key", v.S3Key)
		}
	}

	// Generate fresh variants from the original.
	now := time.Now()
	fileID := strings.TrimSuffix(media.Filename, filepath.Ext(media.Filename))
	pendingVariants, thumbKey := a.generateAndUploadVariants(ctx, original, media.Bucket, fileID, now)

	// Update thumb_s3_key on the media record.
	if thumbKey != nil {
		media.ThumbS3Key = thumbKey
		if err := a.mediaStore.UpdateThumbKey(id, thumbKey); err != nil {
			slog.Warn("update thumb key failed", "error", err, "media_id", id)
		}
	}

	// Store variant records.
	a.saveVariants(id, pendingVariants)

	slog.Info("variants regenerated", "media_id", id, "count", len(pendingVariants))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"count":   len(pendingVariants),
		"message": fmt.Sprintf("Regenerated %d variants.", len(pendingVariants)),
	})
}

// MediaRegenerateBulk regenerates variants for all images that are missing them.
// Uses the ListMediaWithoutVariants query to find candidates and processes
// them sequentially. Returns a JSON summary.
func (a *Admin) MediaRegenerateBulk(w http.ResponseWriter, r *http.Request) {
	if a.storageClient == nil || a.variantStore == nil {
		http.Error(w, "Storage not configured", http.StatusServiceUnavailable)
		return
	}

	sess := middleware.SessionFromCtx(r.Context())

	// Find images without any variants (cap at 50 per request to avoid timeout).
	ids, err := a.variantStore.ListMediaWithoutVariants(50)
	if err != nil {
		slog.Error("list media without variants failed", "error", err)
		writeMediaError(w, "Failed to find images to process.", http.StatusInternalServerError)
		return
	}

	if len(ids) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":        true,
			"processed": 0,
			"message":   "All images already have variants.",
		})
		return
	}

	ctx := r.Context()
	var processed, failed int

	for _, id := range ids {
		media, err := a.mediaStore.FindByID(sess.TenantID, id)
		if err != nil || media == nil {
			failed++
			continue
		}

		original, err := a.storageClient.Download(ctx, media.Bucket, media.S3Key)
		if err != nil {
			slog.Warn("bulk regen: download failed", "error", err, "media_id", id)
			failed++
			continue
		}

		now := time.Now()
		fileID := strings.TrimSuffix(media.Filename, filepath.Ext(media.Filename))
		pendingVariants, thumbKey := a.generateAndUploadVariants(ctx, original, media.Bucket, fileID, now)

		if thumbKey != nil && media.ThumbS3Key == nil {
			media.ThumbS3Key = thumbKey
			_ = a.mediaStore.UpdateThumbKey(id, thumbKey)
		}

		a.saveVariants(id, pendingVariants)
		processed++
	}

	slog.Info("bulk variant regeneration complete", "processed", processed, "failed", failed)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"processed": processed,
		"failed":    failed,
		"remaining": len(ids) - processed - failed,
		"message":   fmt.Sprintf("Processed %d images (%d failed).", processed, failed),
	})
}

// extensionFromType returns a file extension for known MIME types.
func extensionFromType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "application/pdf":
		return ".pdf"
	default:
		return ""
	}
}

// sanitizeSVG removes dangerous elements and attributes from SVG content
// to prevent stored XSS attacks. Strips <script>, <foreignObject>, event
// handlers (onclick, onload, etc.), and javascript: URLs.
func sanitizeSVG(data []byte) []byte {
	p := bluemonday.NewPolicy()

	// Allow SVG structural elements.
	p.AllowElements("svg", "g", "defs", "symbol", "use", "clipPath", "mask", "pattern", "marker", "filter")

	// Allow SVG shape elements.
	p.AllowElements("path", "rect", "circle", "ellipse", "line", "polyline", "polygon", "text", "tspan", "textPath")

	// Allow SVG rendering elements.
	p.AllowElements("linearGradient", "radialGradient", "stop", "image", "title", "desc", "metadata")

	// Allow SVG filter primitives.
	p.AllowElements("feBlend", "feColorMatrix", "feComponentTransfer", "feComposite", "feConvolveMatrix",
		"feDiffuseLighting", "feDisplacementMap", "feDistantLight", "feFlood", "feFuncA", "feFuncB",
		"feFuncG", "feFuncR", "feGaussianBlur", "feImage", "feMerge", "feMergeNode", "feMorphology",
		"feOffset", "fePointLight", "feSpecularLighting", "feSpotLight", "feTile", "feTurbulence")

	// Allow common SVG attributes on all allowed elements.
	svgAttrs := []string{
		"viewBox", "xmlns", "xmlns:xlink", "version", "width", "height",
		"x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry",
		"d", "points", "transform", "fill", "stroke", "stroke-width",
		"stroke-linecap", "stroke-linejoin", "stroke-dasharray", "stroke-dashoffset",
		"opacity", "fill-opacity", "stroke-opacity", "fill-rule", "clip-rule",
		"font-family", "font-size", "font-weight", "font-style",
		"text-anchor", "dominant-baseline", "alignment-baseline",
		"id", "class", "style", "clip-path", "mask", "filter",
		"gradientUnits", "gradientTransform", "spreadMethod", "offset", "stop-color", "stop-opacity",
		"patternUnits", "patternTransform", "preserveAspectRatio",
		"href", "xlink:href", "dx", "dy", "rotate", "textLength",
		"startOffset", "method", "spacing",
		"in", "in2", "result", "stdDeviation", "type", "values", "mode",
		"color-interpolation-filters",
	}
	p.AllowAttrs(svgAttrs...).Globally()

	// Explicitly NOT allowed (stripped by omission):
	// - <script>, <foreignObject>, <iframe>, <embed>, <object>
	// - on* event handlers (onclick, onload, onerror, etc.)
	// - javascript: URLs in href/xlink:href (bluemonday strips these by default)

	return p.SanitizeBytes(data)
}

// writeMediaError writes a JSON error response for media operations.
func writeMediaError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

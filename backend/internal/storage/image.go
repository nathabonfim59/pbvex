package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

var supportedImageTypes = map[string]string{
	"image/gif":  "gif",
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
}

const (
	maxOriginalImageDimension = 16_384
	maxOriginalImagePixels    = 32_000_000
)

type ImagePolicy struct {
	Kind      string   `json:"kind"`
	Thumbs    []string `json:"thumbs"`
	MimeTypes []string `json:"mimeTypes"`
}

type ImageMetadata struct {
	Kind      string   `json:"kind"`
	Extension string   `json:"extension"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Thumbs    []string `json:"thumbs"`
	MimeTypes []string `json:"mimeTypes"`
}

func imagePolicyFromRecord(record *core.Record, field string) (*ImagePolicy, error) {
	raw := record.GetString(field)
	if raw == "" || raw == "null" || raw == "{}" {
		return nil, nil
	}
	var policy ImagePolicy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil || policy.Kind != "image" {
		return nil, fmt.Errorf("invalid stored image policy")
	}
	return &policy, nil
}

func imageMetadataFromRecord(record *core.Record) (*ImageMetadata, error) {
	raw := record.GetString(schema.FieldStorageMetadata)
	if raw == "" || raw == "null" || raw == "{}" {
		return nil, nil
	}
	var metadata ImageMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata.Kind != "image" {
		return nil, fmt.Errorf("invalid stored image metadata")
	}
	return &metadata, nil
}

func validateImagePolicy(policy *ImagePolicy) error {
	if policy == nil || policy.Kind != "image" || len(policy.Thumbs) > 16 || len(policy.MimeTypes) == 0 {
		return fmt.Errorf("invalid image policy")
	}
	seenThumbs := map[string]bool{}
	for _, thumb := range policy.Thumbs {
		match := filesystem.ThumbSizeRegex.FindStringSubmatch(thumb)
		if len(match) == 0 || seenThumbs[thumb] {
			return fmt.Errorf("invalid image thumb %q", thumb)
		}
		seenThumbs[thumb] = true
		width, widthErr := strconv.Atoi(match[1])
		height, heightErr := strconv.Atoi(match[2])
		if widthErr != nil || heightErr != nil || (match[3] != "" && (width == 0 || height == 0)) || (width == 0 && height == 0) || width > 4096 || height > 4096 || (width > 0 && height > 0 && int64(width)*int64(height) > 16_777_216) {
			return fmt.Errorf("invalid image thumb %q", thumb)
		}
	}
	seenTypes := map[string]bool{}
	for _, mimeType := range policy.MimeTypes {
		if _, ok := supportedImageTypes[mimeType]; !ok || seenTypes[mimeType] {
			return fmt.Errorf("unsupported image MIME type %q", mimeType)
		}
		seenTypes[mimeType] = true
	}
	return nil
}

func inspectImage(path string, policy *ImagePolicy) (*ImageMetadata, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	detected, err := mimetype.DetectFile(path)
	if err != nil {
		return nil, "", err
	}
	contentType := detected.String()
	extension, supported := supportedImageTypes[contentType]
	if !supported || !slices.Contains(policy.MimeTypes, contentType) {
		return nil, "", &UploadError{Code: ErrorCodeInvalidContent, Message: "uploaded bytes are not an allowed image"}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return nil, "", &UploadError{Code: ErrorCodeInvalidContent, Message: "uploaded image could not be decoded", Err: err}
	}
	if config.Width > maxOriginalImageDimension || config.Height > maxOriginalImageDimension || int64(config.Width)*int64(config.Height) > maxOriginalImagePixels {
		return nil, "", &UploadError{Code: ErrorCodeInvalidContent, Message: "uploaded image dimensions exceed the allowed limit"}
	}
	for _, thumb := range policy.Thumbs {
		if err := validateThumbForImage(thumb, config.Width, config.Height); err != nil {
			return nil, "", &UploadError{Code: ErrorCodeInvalidContent, Message: err.Error()}
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	if _, _, err := image.Decode(file); err != nil {
		return nil, "", &UploadError{Code: ErrorCodeInvalidContent, Message: "uploaded image could not be decoded", Err: err}
	}
	return &ImageMetadata{Kind: "image", Extension: extension, Width: config.Width, Height: config.Height, Thumbs: slices.Clone(policy.Thumbs), MimeTypes: slices.Clone(policy.MimeTypes)}, contentType, nil
}

func validateThumbForImage(thumb string, originalWidth, originalHeight int) error {
	match := filesystem.ThumbSizeRegex.FindStringSubmatch(thumb)
	if len(match) == 0 || originalWidth <= 0 || originalHeight <= 0 {
		return fmt.Errorf("invalid image thumb %q", thumb)
	}
	width, widthErr := strconv.Atoi(match[1])
	height, heightErr := strconv.Atoi(match[2])
	if widthErr != nil || heightErr != nil || (match[3] != "" && (width == 0 || height == 0)) {
		return fmt.Errorf("invalid image thumb %q", thumb)
	}
	if width == 0 {
		width = int((int64(originalWidth)*int64(height) + int64(originalHeight) - 1) / int64(originalHeight))
	} else if height == 0 {
		height = int((int64(originalHeight)*int64(width) + int64(originalWidth) - 1) / int64(originalWidth))
	}
	if width <= 0 || height <= 0 || width > 4096 || height > 4096 || int64(width)*int64(height) > 16_777_216 {
		return fmt.Errorf("image aspect ratio makes thumb %q exceed the allowed dimensions", thumb)
	}
	return nil
}

func requestedThumb(record *core.Record, request *http.Request) (string, *ImageMetadata, error) {
	values, exists := request.URL.Query()["thumb"]
	if !exists {
		return "", nil, nil
	}
	if len(values) != 1 || values[0] == "" {
		return "", nil, &UploadError{Code: ErrorCodeBadRequest, Message: "invalid image thumb"}
	}
	metadata, err := imageMetadataFromRecord(record)
	if err != nil || metadata == nil || !slices.Contains(metadata.Thumbs, values[0]) {
		return "", nil, &UploadError{Code: ErrorCodeNotFound, Message: "image thumb not found", Err: err}
	}
	return values[0], metadata, nil
}

func (s *Service) ensureThumb(ctx context.Context, fs *filesystem.System, storageID, originalKey, thumb string) (string, error) {
	thumbKey := strings.TrimSuffix(originalKey, "/blob") + "/thumbs/" + thumb + "/blob"
	_, err, _ := s.thumbPending.Do(thumbKey, func() (any, error) {
		if _, err := s.repo.GetFile(schema.WithInternalContext(ctx), s.app, storageID); err != nil {
			return nil, err
		}
		if exists, err := fs.Exists(thumbKey); err != nil || exists {
			return nil, err
		}
		if err := s.thumbSem.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		defer s.thumbSem.Release(1)
		if err := fs.CreateThumb(originalKey, thumbKey, thumb); err != nil {
			return nil, err
		}
		if _, err := s.repo.GetFile(schema.WithInternalContext(ctx), s.app, storageID); err != nil {
			_ = fs.Delete(thumbKey)
			return nil, err
		}
		return nil, nil
	})
	s.thumbPending.Forget(thumbKey)
	return thumbKey, err
}

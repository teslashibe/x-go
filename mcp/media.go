package mcp

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// UploadMediaInput is the typed input for x_upload_media. Provide either an
// image_url (fetched by the backend) or base64-encoded image bytes.
type UploadMediaInput struct {
	ImageURL    string `json:"image_url,omitempty" jsonschema:"description=URL of the image to fetch and upload (png/jpeg/webp/gif)"`
	ImageBase64 string `json:"image_base64,omitempty" jsonschema:"description=base64-encoded image bytes (alternative to image_url)"`
	MimeType    string `json:"mime_type,omitempty" jsonschema:"description=MIME type for image_base64 (e.g. image/png); ignored for image_url"`
	AltText     string `json:"alt_text,omitempty" jsonschema:"description=optional accessibility alt text for the media"`
}

func uploadMedia(ctx context.Context, c *x.Client, in UploadMediaInput) (any, error) {
	var opts []x.MediaOption
	if in.AltText != "" {
		opts = append(opts, x.WithAltText(in.AltText))
	}

	var (
		mediaID string
		err     error
	)
	switch {
	case in.ImageURL != "":
		mediaID, err = c.UploadMediaFromURL(ctx, in.ImageURL, opts...)
	case in.ImageBase64 != "":
		data, decErr := base64.StdEncoding.DecodeString(in.ImageBase64)
		if decErr != nil {
			return nil, fmt.Errorf("invalid base64 image bytes: %w", decErr)
		}
		mediaID, err = c.UploadMedia(ctx, data, in.MimeType, opts...)
	default:
		return nil, fmt.Errorf("provide either image_url or image_base64")
	}
	// A non-empty media_id with an error means the upload succeeded but a
	// non-fatal step (alt text) failed; surface the usable id plus a warning
	// so the agent can still attach it without re-uploading.
	if err != nil {
		if mediaID == "" {
			return nil, err
		}
		return map[string]any{"ok": true, "media_id": mediaID, "warning": err.Error()}, nil
	}
	return map[string]any{"ok": true, "media_id": mediaID}, nil
}

var mediaTools = []mcptool.Tool{
	mcptool.Define[*x.Client, UploadMediaInput](
		"x_upload_media",
		"Upload an image (URL or base64) to X and return its media_id for attaching to posts",
		"UploadMedia",
		uploadMedia,
	),
}

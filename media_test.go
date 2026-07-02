package x

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

// roundTripFunc adapts a function to http.RoundTripper for mock transports.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newTestClient builds a Client with a mock transport, bypassing New()'s
// network session validation.
func newTestClient(rt http.RoundTripper) *Client {
	return &Client{
		cookies:    Cookies{AuthToken: "auth123", CT0: "csrf123", Twid: "u=42"},
		httpClient: &http.Client{Transport: rt, Timeout: 5 * time.Second},
		userAgent:  defaultUserAgent,
		minGap:     0,
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestUploadMediaRequestShaping(t *testing.T) {
	var gotUpload *http.Request
	var uploadBody []byte

	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/i/media/upload.json") {
			gotUpload = r
			uploadBody, _ = io.ReadAll(r.Body)
			return jsonResponse(http.StatusOK, `{"media_id":123,"media_id_string":"123"}`), nil
		}
		t.Fatalf("unexpected request to %s", r.URL)
		return nil, nil
	}))

	id, err := c.UploadMedia(context.Background(), []byte("\x89PNGfakebytes"), "image/png")
	if err != nil {
		t.Fatalf("UploadMedia error: %v", err)
	}
	if id != "123" {
		t.Fatalf("media_id = %q, want 123", id)
	}

	if gotUpload.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", gotUpload.Method)
	}
	if gotUpload.Host != "upload.twitter.com" {
		t.Errorf("host = %s, want upload.twitter.com", gotUpload.Host)
	}
	if got := gotUpload.Header.Get("X-Csrf-Token"); got != "csrf123" {
		t.Errorf("X-Csrf-Token = %q, want csrf123", got)
	}
	if ck := gotUpload.Header.Get("Cookie"); !strings.Contains(ck, "auth_token=auth123") {
		t.Errorf("Cookie header missing auth_token: %q", ck)
	}

	// Multipart body must carry the media part and category.
	mediaType, params, err := mime.ParseMediaType(gotUpload.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		t.Fatalf("Content-Type = %q, want multipart", gotUpload.Header.Get("Content-Type"))
	}
	mr := multipart.NewReader(strings.NewReader(string(uploadBody)), params["boundary"])
	fields := map[string]string{}
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(p)
		fields[p.FormName()] = string(b)
	}
	if fields["media_category"] != "tweet_image" {
		t.Errorf("media_category = %q, want tweet_image", fields["media_category"])
	}
	if _, ok := fields["media"]; !ok {
		t.Error("multipart body missing media field")
	}
}

func TestUploadMediaWithAltText(t *testing.T) {
	var metadataHit bool

	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/i/media/upload.json"):
			return jsonResponse(http.StatusOK, `{"media_id_string":"555"}`), nil
		case strings.HasSuffix(r.URL.Path, "/media/metadata/create.json"):
			metadataHit = true
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "hello alt") {
				t.Errorf("metadata body missing alt text: %s", body)
			}
			return jsonResponse(http.StatusOK, `{}`), nil
		}
		t.Fatalf("unexpected request to %s", r.URL)
		return nil, nil
	}))

	if _, err := c.UploadMedia(context.Background(), []byte("jpegbytes"), "image/jpeg", WithAltText("hello alt")); err != nil {
		t.Fatalf("UploadMedia error: %v", err)
	}
	if !metadataHit {
		t.Error("alt text did not trigger metadata/create call")
	}
}

func TestUploadMediaPerTypeSizeLimits(t *testing.T) {
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"media_id_string":"1"}`), nil
	}))

	// A 6 MiB PNG exceeds the 5 MiB image cap.
	png := make([]byte, 6<<20)
	if _, err := c.UploadMedia(context.Background(), png, "image/png"); !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("6MiB png: err = %v, want ErrMediaTooLarge", err)
	}
	// The same 6 MiB payload as a GIF is within the 15 MiB GIF cap.
	if _, err := c.UploadMedia(context.Background(), png, "image/gif"); err != nil {
		t.Fatalf("6MiB gif: unexpected err %v", err)
	}
	// A 16 MiB GIF exceeds the GIF cap.
	bigGif := make([]byte, 16<<20)
	if _, err := c.UploadMedia(context.Background(), bigGif, "image/gif"); !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("16MiB gif: err = %v, want ErrMediaTooLarge", err)
	}
}

func TestUploadMediaUnsupportedType(t *testing.T) {
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("no request should be made for unsupported type")
		return nil, nil
	}))
	_, err := c.UploadMedia(context.Background(), []byte("data"), "application/pdf")
	if !errors.Is(err, ErrUnsupportedMediaType) {
		t.Fatalf("err = %v, want ErrUnsupportedMediaType", err)
	}
}

func TestUploadMediaFromURL(t *testing.T) {
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			resp := jsonResponse(http.StatusOK, "pngbytes")
			resp.Header.Set("Content-Type", "image/png")
			return resp, nil
		}
		return jsonResponse(http.StatusOK, `{"media_id_string":"777"}`), nil
	}))

	id, err := c.UploadMediaFromURL(context.Background(), "https://smore.test/api/uploads/x.png")
	if err != nil {
		t.Fatalf("UploadMediaFromURL error: %v", err)
	}
	if id != "777" {
		t.Fatalf("media_id = %q, want 777", id)
	}
}

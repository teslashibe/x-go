package x

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// videoMock is a scripted upload.twitter.com transport that drives the
// INIT -> APPEND -> FINALIZE -> STATUS chunked state machine.
type videoMock struct {
	t             *testing.T
	mediaID       string
	segments      int      // number of APPEND segments received
	segmentBytes  int      // total bytes across APPEND segments
	commands      []string // ordered non-APPEND commands seen
	finalizeInfo  string   // processing_info JSON returned by FINALIZE (or "")
	statusReplies []string // processing_info JSON returned by successive STATUS calls
	statusIdx     int
}

func (m *videoMock) RoundTrip(r *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(r.URL.Path, "/i/media/upload.json") {
		m.t.Fatalf("unexpected request to %s", r.URL)
	}
	if r.Method == http.MethodGet {
		cmd := r.URL.Query().Get("command")
		m.commands = append(m.commands, cmd)
		body := `{}`
		if m.statusIdx < len(m.statusReplies) {
			body = `{"processing_info":` + m.statusReplies[m.statusIdx] + `}`
		}
		m.statusIdx++
		return jsonResponse(http.StatusOK, body), nil
	}

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		_, params, _ := mime.ParseMediaType(ct)
		mr := multipart.NewReader(r.Body, params["boundary"])
		var cmd string
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			switch p.FormName() {
			case "command":
				cmd = string(b)
			case "media":
				m.segmentBytes += len(b)
			}
		}
		if cmd != "APPEND" {
			m.t.Fatalf("multipart command = %q, want APPEND", cmd)
		}
		m.segments++
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}

	body, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(body))
	cmd := form.Get("command")
	m.commands = append(m.commands, cmd)
	switch cmd {
	case "INIT":
		return jsonResponse(http.StatusOK, `{"media_id_string":"`+m.mediaID+`"}`), nil
	case "FINALIZE":
		if m.finalizeInfo == "" {
			return jsonResponse(http.StatusOK, `{"media_id_string":"`+m.mediaID+`"}`), nil
		}
		return jsonResponse(http.StatusOK, `{"processing_info":`+m.finalizeInfo+`}`), nil
	}
	m.t.Fatalf("unexpected command %q", cmd)
	return nil, nil
}

func TestUploadVideoStateMachine(t *testing.T) {
	m := &videoMock{
		t:            t,
		mediaID:      "9001",
		finalizeInfo: `{"state":"in_progress","check_after_secs":1}`,
		statusReplies: []string{
			`{"state":"succeeded","progress_percent":100}`,
		},
	}
	c := newTestClient(m)

	// 10 MiB source => 3 APPEND segments at 4 MiB chunks.
	size := int64(10 << 20)
	data := strings.NewReader(strings.Repeat("v", int(size)))

	id, err := c.UploadVideo(context.Background(), data, "video/mp4", size)
	if err != nil {
		t.Fatalf("UploadVideo error: %v", err)
	}
	if id != "9001" {
		t.Fatalf("media_id = %q, want 9001", id)
	}
	if m.segments != 3 {
		t.Errorf("APPEND segments = %d, want 3", m.segments)
	}
	if int64(m.segmentBytes) != size {
		t.Errorf("APPEND bytes = %d, want %d", m.segmentBytes, size)
	}
	// INIT before FINALIZE, and at least one STATUS poll after FINALIZE.
	want := []string{"INIT", "FINALIZE", "STATUS"}
	if strings.Join(m.commands, ",") != strings.Join(want, ",") {
		t.Errorf("commands = %v, want %v", m.commands, want)
	}
}

func TestUploadVideoProcessingFailed(t *testing.T) {
	m := &videoMock{
		t:            t,
		mediaID:      "7",
		finalizeInfo: `{"state":"failed","error":{"code":1,"name":"InvalidMedia","message":"unsupported codec"}}`,
	}
	c := newTestClient(m)

	size := int64(1 << 20)
	_, err := c.UploadVideo(context.Background(), strings.NewReader(strings.Repeat("v", int(size))), "video/mp4", size)
	if !errors.Is(err, ErrMediaProcessingFailed) {
		t.Fatalf("err = %v, want ErrMediaProcessingFailed", err)
	}
	if !strings.Contains(err.Error(), "unsupported codec") {
		t.Errorf("err %q should surface X's reason", err)
	}
}

func TestUploadVideoUnsupportedType(t *testing.T) {
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("no request should be made for unsupported type")
		return nil, nil
	}))
	_, err := c.UploadVideo(context.Background(), strings.NewReader("x"), "video/avi", 1)
	if !errors.Is(err, ErrUnsupportedMediaType) {
		t.Fatalf("err = %v, want ErrUnsupportedMediaType", err)
	}
}

func TestUploadVideoTooLarge(t *testing.T) {
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("no request should be made when over the size cap")
		return nil, nil
	}))
	_, err := c.UploadVideo(context.Background(), strings.NewReader("x"), "video/mp4", maxVideoUploadBytes+1)
	if !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("err = %v, want ErrMediaTooLarge", err)
	}
}

func TestUploadVideoSizeMismatch(t *testing.T) {
	m := &videoMock{t: t, mediaID: "5"}
	c := newTestClient(m)
	// Declared size larger than the actual reader payload must fail closed.
	_, err := c.UploadVideo(context.Background(), strings.NewReader("short"), "video/mp4", 1<<20)
	if !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("err = %v, want ErrInvalidParams", err)
	}
}

func TestUploadVideoFinalizeNoProcessing(t *testing.T) {
	// FINALIZE with no processing_info means the media is immediately usable.
	m := &videoMock{t: t, mediaID: "42"}
	c := newTestClient(m)
	size := int64(2 << 20)
	id, err := c.UploadVideo(context.Background(), strings.NewReader(strings.Repeat("v", int(size))), "video/quicktime", size)
	if err != nil {
		t.Fatalf("UploadVideo error: %v", err)
	}
	if id != "42" {
		t.Fatalf("media_id = %q, want 42", id)
	}
	for _, cmd := range m.commands {
		if cmd == "STATUS" {
			t.Error("no STATUS poll expected when FINALIZE omits processing_info")
		}
	}
}

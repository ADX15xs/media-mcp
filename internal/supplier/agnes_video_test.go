package supplier

import (
	"encoding/json"
	"fmt"
	"io"
	"media-mcp/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestAgnesVideo(baseURL string) *AgnesVideoAdapter {
	a := NewAgnesVideoAdapter(&config.SupplierConfig{
		BaseURL: baseURL + "/v1/videos",
		APIKey:  "test-key",
		Model:   "agnes-video-v2.0",
	})
	a.timing = pollTiming{
		initialWait:  0,
		base:         time.Millisecond,
		min:          time.Millisecond,
		max:          5 * time.Millisecond,
		totalTimeout: 10 * time.Second,
		grace:        200 * time.Millisecond,
	}
	return a
}

func TestParseAgnesStatus_agnesapiShape(t *testing.T) {
	body := []byte(`{"id":"video_abc","status":"completed","progress":100,"seconds":"5.0",
		"url":"https://cdn.example.com/v.mp4","request_params":{"frame_rate":24,"num_frames":121}}`)

	st, err := parseAgnesStatus(body)
	if err != nil {
		t.Fatalf("parseAgnesStatus() error = %v", err)
	}
	if st.Status != "completed" {
		t.Errorf("Status = %q, want completed", st.Status)
	}
	if st.URL != "https://cdn.example.com/v.mp4" {
		t.Errorf("URL = %q", st.URL)
	}
	if st.Seconds != 5.0 {
		t.Errorf("Seconds = %v, want 5 (from string \"5.0\")", st.Seconds)
	}
	if st.FrameRate != 24 {
		t.Errorf("FrameRate = %v, want 24 (from request_params)", st.FrameRate)
	}
}

func TestParseAgnesStatus_taskShapeExposesVideoID(t *testing.T) {
	body := []byte(`{"id":"task_abc","status":"in_progress","progress":30,"video_id":"video_xyz","seconds":"3.4"}`)

	st, err := parseAgnesStatus(body)
	if err != nil {
		t.Fatalf("parseAgnesStatus() error = %v", err)
	}
	if st.VideoID != "video_xyz" {
		t.Errorf("VideoID = %q, want video_xyz", st.VideoID)
	}
	if st.URL != "" {
		t.Errorf("URL = %q, want empty (task endpoint never carries it)", st.URL)
	}
}

func TestParseAgnesStatus_internalStatusFallback(t *testing.T) {
	st, err := parseAgnesStatus([]byte(`{"internal_status":"inference","internal_progress":30}`))
	if err != nil {
		t.Fatalf("parseAgnesStatus() error = %v", err)
	}
	if st.Status != "inference" {
		t.Errorf("Status = %q, want inference", st.Status)
	}
}

func TestErrMessageFrom(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"nested", `{"error":{"code":404,"message":"task not found"}}`, "task not found"},
		{"flat", `{"error":"boom"}`, "boom"},
		{"field", `{"error_message":"bad prompt"}`, "bad prompt"},
		{"null", `{"error":null}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, err := parseAgnesStatus([]byte(c.body))
			if err != nil {
				t.Fatalf("parse error = %v", err)
			}
			if st.ErrMsg != c.want {
				t.Errorf("ErrMsg = %q, want %q", st.ErrMsg, c.want)
			}
		})
	}
}

// A transient 404 on the status endpoint must not discard a task the backend
// goes on to complete — this is the regression that lost three finished videos.
func TestPollTask_transient404DoesNotAbandonTask(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/agnesapi") {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusInternalServerError)
			return
		}
		switch atomic.AddInt32(&calls, 1) {
		case 1, 2:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"code":404,"message":"task not found"}}`)
		case 3:
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"code":429,"message":"video status query rate limit exceeded"}}`)
		default:
			fmt.Fprint(w, `{"status":"completed","progress":100,"seconds":"5.0","url":"https://cdn.example.com/ok.mp4"}`)
		}
	}))
	defer srv.Close()

	a := newTestAgnesVideo(srv.URL)
	res := a.pollTask("task_1", "video_1", "agnes-video-v2.0", VideoRequest{Prompt: "x"})

	if res.Error != nil {
		t.Fatalf("pollTask() error = %v, want recovery after transient failures", res.Error)
	}
	if len(res.URLs) != 1 || res.URLs[0] != "https://cdn.example.com/ok.mp4" {
		t.Fatalf("URLs = %v", res.URLs)
	}
	if atomic.LoadInt32(&calls) < 4 {
		t.Errorf("calls = %d, want the poller to have retried past the 404/429", calls)
	}
}

// When agnesapi is down, the task endpoint keeps the poll alive and reveals the
// video_id needed to fetch the final URL.
func TestPollTask_fallsBackToTaskEndpoint(t *testing.T) {
	var agnesCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/agnesapi") {
			if atomic.AddInt32(&agnesCalls, 1) <= 1 {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":{"code":404,"message":"task not found"}}`)
				return
			}
			fmt.Fprint(w, `{"status":"completed","url":"https://cdn.example.com/late.mp4"}`)
			return
		}
		fmt.Fprint(w, `{"id":"task_1","status":"completed","progress":100,"video_id":"video_discovered","seconds":"5.0"}`)
	}))
	defer srv.Close()

	a := newTestAgnesVideo(srv.URL)
	res := a.pollTask("task_1", "", "agnes-video-v2.0", VideoRequest{Prompt: "x"})

	if res.Error != nil {
		t.Fatalf("pollTask() error = %v", res.Error)
	}
	if len(res.URLs) != 1 || res.URLs[0] != "https://cdn.example.com/late.mp4" {
		t.Fatalf("URLs = %v, want URL resolved via discovered video_id", res.URLs)
	}
}

func TestPollTask_failedStatusIsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"failed","error":{"message":"nsfw prompt"}}`)
	}))
	defer srv.Close()

	a := newTestAgnesVideo(srv.URL)
	res := a.pollTask("task_1", "video_1", "agnes-video-v2.0", VideoRequest{Prompt: "x"})

	if res.Error == nil {
		t.Fatal("pollTask() error = nil, want terminal failure")
	}
	if !strings.Contains(res.Error.Error(), "nsfw prompt") {
		t.Errorf("error = %v, want the API message surfaced", res.Error)
	}
}

// Every terminal error must carry the handles needed to recover the output.
func TestAbandonIncludesRecoveryHandles(t *testing.T) {
	a := newTestAgnesVideo("https://example.com")
	res := a.abandon(VideoRequest{Prompt: "x"}, "m", "task_9", "video_9", fmt.Errorf("boom"))

	msg := res.Error.Error()
	for _, want := range []string{"task_9", "video_9", "re-attach"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
}

// Re-attaching must not re-submit a generation.
func TestGenVideo_resumeSkipsCreation(t *testing.T) {
	var posts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&posts, 1)
		}
		fmt.Fprint(w, `{"status":"completed","progress":100,"seconds":"5.0","url":"https://cdn.example.com/resumed.mp4"}`)
	}))
	defer srv.Close()

	a := newTestAgnesVideo(srv.URL)
	res := a.GenVideo(VideoRequest{Prompt: "x", Extra: map[string]interface{}{"video_id": "video_1"}})

	if res.Error != nil {
		t.Fatalf("GenVideo() error = %v", res.Error)
	}
	if posts != 0 {
		t.Errorf("POST count = %d, want 0 (resume must not create a new task)", posts)
	}
	if len(res.URLs) != 1 || res.URLs[0] != "https://cdn.example.com/resumed.mp4" {
		t.Errorf("URLs = %v", res.URLs)
	}
}

func TestGenVideo_sizeMapping(t *testing.T) {
	var posts int32
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&posts, 1)
			b, _ := io.ReadAll(r.Body)
			lastBody = string(b)
			fmt.Fprint(w, `{"task_id":"task_1","video_id":"video_1"}`)
			return
		}
		fmt.Fprint(w, `{"status":"completed","progress":100,"seconds":"5.0","url":"https://cdn.example.com/ok.mp4"}`)
	}))
	defer srv.Close()

	a := newTestAgnesVideo(srv.URL)
	cases := []struct {
		name         string
		ar, res      string
		wantW, wantH int
	}{
		{"vertical-default-res", "9:16", "", 720, 1280},
		{"vertical-1080p", "9:16", "1080p", 1080, 1920},
		{"landscape-default-res", "16:9", "", 1280, 720},
		{"resolution-only-16x9-default", "", "1080p", 1920, 1080},
		{"square-720p", "1:1", "720p", 720, 720},
		{"none-keeps-config-default", "", "", 1152, 768},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := a.GenVideo(VideoRequest{Prompt: "x", AspectRatio: c.ar, Resolution: c.res})
			if res.Error != nil {
				t.Fatalf("GenVideo() error = %v", res.Error)
			}
			var payload struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			}
			if err := json.Unmarshal([]byte(lastBody), &payload); err != nil {
				t.Fatalf("parse last body: %v (%s)", err, lastBody)
			}
			if payload.Width != c.wantW || payload.Height != c.wantH {
				t.Errorf("payload = %dx%d, want %dx%d (body: %s)", payload.Width, payload.Height, c.wantW, c.wantH, lastBody)
			}
			if strings.Contains(lastBody, "aspect_ratio") || strings.Contains(lastBody, "resolution") {
				t.Errorf("size control fields leaked into upstream payload: %s", lastBody)
			}
		})
	}
}

func TestGenVideo_invalidSizeValuesRejected(t *testing.T) {
	var posts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&posts, 1)
		}
		fmt.Fprint(w, `{"status":"completed","progress":100,"seconds":"5.0","url":"https://cdn.example.com/ok.mp4"}`)
	}))
	defer srv.Close()

	a := newTestAgnesVideo(srv.URL)
	cases := []struct {
		name    string
		ar, res string
		want    string
	}{
		{"bad-ratio", "5:2", "", "unsupported aspect_ratio"},
		{"bad-res", "", "999p", "unsupported resolution"},
		{"bad-res-with-ratio", "9:16", "4k", "unsupported resolution"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := a.GenVideo(VideoRequest{Prompt: "x", AspectRatio: c.ar, Resolution: c.res})
			if res.Error == nil {
				t.Fatal("GenVideo() error = nil, want rejection before POST")
			}
			if !strings.Contains(res.Error.Error(), c.want) {
				t.Errorf("error = %v, want containing %q", res.Error, c.want)
			}
		})
	}
	if posts != 0 {
		t.Errorf("POST count = %d, want 0 (invalid size must be rejected before submission)", posts)
	}
}

func TestFloatFrom(t *testing.T) {
	cases := []struct {
		in   interface{}
		want float64
	}{
		{"5.0", 5},
		{float64(24), 24},
		{7, 7},
		{"nope", 0},
		{nil, 0},
	}
	for _, c := range cases {
		if got := floatFrom(c.in); got != c.want {
			t.Errorf("floatFrom(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

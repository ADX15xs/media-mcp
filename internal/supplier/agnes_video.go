package supplier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"media-mcp/internal/config"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AgnesVideoAdapter implements VideoSupplier for Agnes AI video generation API.
type AgnesVideoAdapter struct {
	BaseURL       string
	APIKey        string
	Model         string
	Width         int
	Height        int
	NumFrames     int
	FrameRate     int
	CustomHeaders map[string]string
	ExtraFields   map[string]interface{}
	Config        *config.SupplierConfig

	timing pollTiming
}

// pollTiming groups the polling schedule so it can be tuned per deployment and
// compressed in tests.
type pollTiming struct {
	base time.Duration
	min  time.Duration
	max  time.Duration
}

func defaultPollTiming() pollTiming {
	return pollTiming{
		base: 8 * time.Second,
		min:  5 * time.Second,
		max:  30 * time.Second,
	}
}

func NewAgnesVideoAdapter(cfg *config.SupplierConfig) *AgnesVideoAdapter {
	width := 1152
	height := 768
	numFrames := 121
	frameRate := 24

	if v := intFromExtra(cfg.Extra["width"]); v > 0 {
		width = v
	}
	if v := intFromExtra(cfg.Extra["height"]); v > 0 {
		height = v
	}
	if v := intFromExtra(cfg.Extra["num_frames"]); v > 0 {
		numFrames = v
	}
	if v := intFromExtra(cfg.Extra["frame_rate"]); v > 0 {
		frameRate = v
	}

	timing := defaultPollTiming()
	if v := intFromExtra(cfg.Extra["poll_interval_seconds"]); v > 0 {
		timing.base = time.Duration(v) * time.Second
	}

	return &AgnesVideoAdapter{
		BaseURL:       cfg.BaseURL,
		APIKey:        cfg.APIKey,
		Model:         cfg.Model,
		Width:         width,
		Height:        height,
		NumFrames:     numFrames,
		FrameRate:     frameRate,
		CustomHeaders: cfg.Headers,
		ExtraFields:   cfg.Extra,
		Config:        cfg,
		timing:        timing,
	}
}

func (a *AgnesVideoAdapter) Name() string {
	return "agnes_video"
}

// Capabilities exposes the constraints agents must respect before submitting:
// creation is rate-limited to 1 req/min, duration is capped at ~18s, output
// pixels are 32-aligned, and video_id is the preferred polling key.
func (a *AgnesVideoAdapter) Capabilities() string {
	return "Rate limit: creation is limited to 1 request per minute; submit tasks sequentially with >= 60s between calls. " +
		"Duration is capped at ~18s (num_frames <= 441 at 24fps); longer durations are clamped. " +
		"Output dimensions are 32-aligned; the exact size is not guaranteed. " +
		"Tasks are keyed by video_id; prefer passing video_id when polling."
}

// videoSizeTable maps aspect_ratio × resolution to backend width/height.
// Agnes keeps the ratio but re-aligns pixels to multiples of 32 (probed
// 2026-08-06: 720x1280 -> 704x1280, 1080x1920 -> 1088x1920).
var videoSizeTable = SizeTable{
	"9:16": {"480p": {480, 854}, "720p": {720, 1280}, "1080p": {1080, 1920}},
	"16:9": {"480p": {854, 480}, "720p": {1280, 720}, "1080p": {1920, 1080}},
	"1:1":  {"480p": {480, 480}, "720p": {720, 720}, "1080p": {1080, 1080}},
	"4:3":  {"480p": {640, 480}, "720p": {960, 720}, "1080p": {1440, 1080}},
	"3:4":  {"480p": {480, 640}, "720p": {720, 960}, "1080p": {1080, 1440}},
}

// GenVideo submits an Agnes AI video generation task and returns immediately
// with a working result carrying the task handle. Poll completion with
// GetVideoResult (exposed as the _getVideoResult tool); the task is never
// re-created.
func (a *AgnesVideoAdapter) GenVideo(req VideoRequest) *VideoResult {
	modelUsed := defaultStr(req.Model, a.Model)

	numFrames := a.NumFrames
	// Agnes controls length via num_frames (must be 8n+1, <=441), not duration.
	if req.Duration > 0 {
		numFrames = fracToNumFrames(req.Duration, a.FrameRate)
	}

	// Per-request size override; keep in locals, never touch the shared
	// Width/Height (tools/call runs concurrently).
	width, height := a.Width, a.Height
	if req.AspectRatio != "" || req.Resolution != "" {
		ar := defaultStr(req.AspectRatio, "16:9")
		res := defaultStr(req.Resolution, "720p")
		w, h, err := videoSizeTable.ResolveSize(ar, res)
		if err != nil {
			return &VideoResult{Request: req, Error: err}
		}
		width, height = w, h
	}

	payload := map[string]interface{}{
		"model":      modelUsed,
		"prompt":     req.Prompt,
		"width":      width,
		"height":     height,
		"num_frames": numFrames,
		"frame_rate": a.FrameRate,
	}

	if req.Style != "" {
		payload["style"] = req.Style
	}
	if req.Seed != nil {
		payload["seed"] = *req.Seed
	}

	for k, v := range a.ExtraFields {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return &VideoResult{Request: req, Error: fmt.Errorf("marshal request: %w", err)}
	}

	client := &http.Client{Timeout: 60 * time.Second}
	endpoint := strings.TrimSuffix(a.BaseURL, "/")

	logf("video create: POST %s model=%s num_frames=%d frame_rate=%d %dx%d",
		endpoint, modelUsed, numFrames, a.FrameRate, width, height)
	debugf("video create payload: %s", truncate(string(jsonData), 1500))

	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return &VideoResult{Request: req, Error: fmt.Errorf("create request: %w", err)}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)
	for k, v := range a.CustomHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return &VideoResult{Request: req, Error: fmt.Errorf("HTTP request: %w", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &VideoResult{Request: req, Error: fmt.Errorf("read response: %w", err)}
	}

	debugf("video create response HTTP %d: %s", resp.StatusCode, truncate(string(body), 1500))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		hint := ""
		if resp.StatusCode == 429 {
			hint = " (Agnes rate-limits video creation; submit tasks sequentially rather than in parallel)"
		}
		return &VideoResult{Request: req, Error: fmt.Errorf("HTTP %d: %s%s", resp.StatusCode, truncate(string(body), 200), hint)}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return &VideoResult{Request: req, Error: fmt.Errorf("parse creation response: %w (body: %s)", err, truncate(string(body), 200))}
	}

	if msg := errMessageFrom(raw); msg != "" {
		return &VideoResult{Request: req, Error: fmt.Errorf("API error: %s", msg)}
	}

	taskID, _ := raw["task_id"].(string)
	if taskID == "" {
		taskID, _ = raw["id"].(string)
	}
	videoID, _ := raw["video_id"].(string)

	if taskID == "" && videoID == "" {
		return &VideoResult{Request: req, Error: fmt.Errorf("no task_id/video_id in response: %s", truncate(string(body), 200))}
	}

	logf("video submitted: task_id=%s video_id=%s", orNone(taskID), orNone(videoID))
	return &VideoResult{
		Request:   req,
		ModelUsed: modelUsed,
		Status:    "working",
		TaskID:    taskID,
		VideoID:   videoID,
	}
}

// agnesStatus is the normalized view of both Agnes status endpoints, which
// return overlapping but differently-shaped payloads.
type agnesStatus struct {
	Status    string
	Progress  float64
	URL       string
	VideoID   string
	Seconds   float64
	FrameRate float64
	ErrMsg    string
}

func parseAgnesStatus(body []byte) (agnesStatus, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return agnesStatus{}, err
	}

	st := agnesStatus{}
	st.Status, _ = raw["status"].(string)
	if st.Status == "" {
		st.Status, _ = raw["internal_status"].(string)
	}
	st.Progress = floatFrom(raw["progress"])
	st.VideoID, _ = raw["video_id"].(string)
	st.Seconds = floatFrom(raw["seconds"])
	st.ErrMsg = errMessageFrom(raw)

	for _, k := range []string{"url", "video_url", "result_url"} {
		if v, ok := raw[k].(string); ok && v != "" {
			st.URL = v
			break
		}
	}
	if st.URL == "" {
		if md, ok := raw["metadata"].(map[string]interface{}); ok {
			st.URL, _ = md["url"].(string)
		}
	}

	for _, k := range []string{"request_params", "perf_params"} {
		if p, ok := raw[k].(map[string]interface{}); ok {
			if fr := floatFrom(p["frame_rate"]); fr > 0 {
				st.FrameRate = fr
				break
			}
		}
	}
	if st.FrameRate == 0 {
		st.FrameRate = floatFrom(raw["frame_rate"])
	}

	return st, nil
}

type pollEndpoint struct {
	label string
	url   string
	// carriesURL marks endpoints that expose the finished video URL.
	// /v1/videos/<task_id> reports status only, never the URL.
	carriesURL bool
}

// pollTask probes the Agnes status endpoints until the task reaches a terminal
// state or cap elapses. On cap it returns a working result (no error) so the
// caller retries with another getVideoResult call. Both status endpoints are
// flaky under load (404/429 for tasks that later complete); every HTTP failure
// is treated as transient and retried within the cap.
func (a *AgnesVideoAdapter) pollTask(taskID, videoID, modelUsed string, req VideoRequest, cap time.Duration) *VideoResult {
	host := strings.TrimSuffix(strings.TrimSuffix(a.BaseURL, "/"), "/v1/videos")
	host = strings.TrimSuffix(host, "/")

	client := &http.Client{Timeout: 30 * time.Second}
	tm := a.timing

	deadline := time.Now().Add(cap)
	interval := tm.base
	lastStatus := ""
	lastProgress := 0.0
	lastProgressAt := time.Now()

	for {
		if time.Now().After(deadline) {
			return &VideoResult{Request: req, ModelUsed: modelUsed, Status: "working", TaskID: taskID, VideoID: videoID}
		}

		st, ok, failure := a.probe(client, host, taskID, videoID)
		if !ok {
			logf("video poll transient failure (%s), retrying in %v", failure, interval)
			time.Sleep(interval)
			interval = growInterval(interval, tm.max)
			continue
		}

		// /v1/videos/<task_id> reveals the video_id even when creation did not.
		if videoID == "" && st.VideoID != "" {
			videoID = st.VideoID
			logf("video poll: discovered video_id=%s for task_id=%s", videoID, taskID)
		}

		if st.Status != lastStatus {
			lastStatus = st.Status
			logf("video status [%s]: %s (progress: %.0f%%)", orNone(taskID), orNone(st.Status), st.Progress)
		}

		// Adapt the polling interval to progress velocity.
		if st.Progress > lastProgress {
			velocity := (st.Progress - lastProgress) / time.Since(lastProgressAt).Seconds()
			switch {
			case velocity >= 10:
				interval = tm.min
			case velocity >= 2:
				interval = tm.base
			default:
				interval = tm.max
			}
			lastProgress = st.Progress
			lastProgressAt = time.Now()
		}

		switch st.Status {
		case "completed", "succeeded", "success":
			videoURL := st.URL
			if videoURL == "" {
				videoURL = a.resolveURL(client, host, videoID)
			}
			if videoURL == "" {
				return &VideoResult{Request: req, ModelUsed: modelUsed, Status: "failed", TaskID: taskID, VideoID: videoID,
					Error: fmt.Errorf("task completed but no video URL could be resolved")}
			}
			logf("video completed [%s]: %s", orNone(taskID), videoURL)
			return &VideoResult{
				Request:   req,
				ModelUsed: modelUsed,
				Status:    "completed",
				URLs:      []string{videoURL},
				FrameRate: st.FrameRate,
				Duration:  int(st.Seconds + 0.5),
			}

		case "failed", "error", "failure", "cancelled", "canceled":
			return &VideoResult{Request: req, ModelUsed: modelUsed, Status: "failed", TaskID: taskID, VideoID: videoID,
				Error: fmt.Errorf("video generation failed: %s (task_id=%s)", defaultStr(st.ErrMsg, "unknown error"), orNone(taskID))}

		default:
			time.Sleep(interval)
		}
	}
}

// GetVideoResult polls an existing Agnes video task for up to statusPollCap.
// task_id and/or video_id must be supplied; the task is never re-created.
func (a *AgnesVideoAdapter) GetVideoResult(taskID, videoID string) *VideoResult {
	if taskID == "" && videoID == "" {
		return &VideoResult{Error: fmt.Errorf("task_id or video_id required")}
	}
	req := VideoRequest{Supplier: a.Name()}
	return a.pollTask(taskID, videoID, a.Model, req, statusPollCap)
}

// probe queries the status endpoints in order of usefulness and returns the
// first usable answer. ok=false means every endpoint failed this round.
func (a *AgnesVideoAdapter) probe(client *http.Client, host, taskID, videoID string) (agnesStatus, bool, string) {
	var failures []string

	for _, ep := range a.endpoints(host, taskID, videoID) {
		body, code, err := a.get(client, ep.url)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", ep.label, err))
			continue
		}
		debugf("video poll %s -> HTTP %d: %s", ep.label, code, truncate(string(body), 600))

		if code < 200 || code >= 300 {
			failures = append(failures, fmt.Sprintf("%s: HTTP %d %s", ep.label, code, truncate(string(body), 120)))
			continue
		}
		st, parseErr := parseAgnesStatus(body)
		if parseErr != nil {
			failures = append(failures, fmt.Sprintf("%s: parse %v", ep.label, parseErr))
			continue
		}
		if st.Status == "" {
			failures = append(failures, fmt.Sprintf("%s: no status field", ep.label))
			continue
		}
		// A status-only endpoint reporting completion is still useful; the URL
		// gets resolved separately.
		if !ep.carriesURL && st.Status == "completed" && st.URL == "" {
			debugf("video poll %s reported completed without URL; will resolve via agnesapi", ep.label)
		}
		return st, true, ""
	}

	return agnesStatus{}, false, strings.Join(failures, "; ")
}

func (a *AgnesVideoAdapter) endpoints(host, taskID, videoID string) []pollEndpoint {
	var eps []pollEndpoint
	if videoID != "" {
		eps = append(eps, pollEndpoint{
			label:      "agnesapi(video_id)",
			url:        fmt.Sprintf("%s/agnesapi?video_id=%s", host, url.QueryEscape(videoID)),
			carriesURL: true,
		})
	}
	if taskID != "" {
		eps = append(eps, pollEndpoint{
			label: "v1/videos(task_id)",
			url:   fmt.Sprintf("%s/v1/videos/%s", host, url.PathEscape(taskID)),
		})
	}
	return eps
}

// resolveURL fetches the finished video URL from agnesapi, which is the only
// endpoint that carries it. Retried briefly because the URL can lag the
// completed status by a few seconds.
func (a *AgnesVideoAdapter) resolveURL(client *http.Client, host, videoID string) string {
	if videoID == "" {
		return ""
	}
	target := fmt.Sprintf("%s/agnesapi?video_id=%s", host, url.QueryEscape(videoID))
	for attempt := 1; attempt <= 5; attempt++ {
		body, code, err := a.get(client, target)
		if err == nil && code >= 200 && code < 300 {
			if st, parseErr := parseAgnesStatus(body); parseErr == nil && st.URL != "" {
				return st.URL
			}
		}
		debugf("video URL resolve attempt %d/5 failed (HTTP %d, err=%v)", attempt, code, err)
		time.Sleep(time.Duration(attempt) * a.timing.min / 2)
	}
	return ""
}

func (a *AgnesVideoAdapter) get(client *http.Client, target string) ([]byte, int, error) {
	httpReq, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return nil, 0, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)
	for k, v := range a.CustomHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func growInterval(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	return next
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func floatFrom(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f
		}
	}
	return 0
}

// errMessageFrom extracts an error message from the several shapes Agnes uses:
// {"error":"msg"}, {"error":{"message":"msg"}} and {"error_message":"msg"}.
func errMessageFrom(raw map[string]interface{}) string {
	if m, ok := raw["error_message"].(string); ok && m != "" {
		return m
	}
	switch e := raw["error"].(type) {
	case string:
		return e
	case map[string]interface{}:
		if m, ok := e["message"].(string); ok {
			return m
		}
	}
	return ""
}

func init() {
	RegisterVideo("agnes_video", func(cfg *config.SupplierConfig) (VideoSupplier, error) {
		return NewAgnesVideoAdapter(cfg), nil
	})
}

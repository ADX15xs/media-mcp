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
	initialWait  time.Duration
	base         time.Duration
	min          time.Duration
	max          time.Duration
	totalTimeout time.Duration
	// grace is how long every status endpoint may keep failing before the task
	// is abandoned. It must comfortably exceed a typical generation so that a
	// transient 404/429 storm can never discard a task that is still running.
	grace time.Duration
}

func defaultPollTiming() pollTiming {
	return pollTiming{
		initialWait:  15 * time.Second,
		base:         8 * time.Second,
		min:          5 * time.Second,
		max:          30 * time.Second,
		totalTimeout: 15 * time.Minute,
		grace:        3 * time.Minute,
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
	if v := intFromExtra(cfg.Extra["initial_wait_seconds"]); v > 0 {
		timing.initialWait = time.Duration(v) * time.Second
	}
	if v := intFromExtra(cfg.Extra["poll_interval_seconds"]); v > 0 {
		timing.base = time.Duration(v) * time.Second
	}
	if v := intFromExtra(cfg.Extra["max_timeout_seconds"]); v > 0 {
		timing.totalTimeout = time.Duration(v) * time.Second
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

func (a *AgnesVideoAdapter) ExtraInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"task_id": map[string]interface{}{
			"type":        "string",
			"description": "Optional: re-attach to an existing task instead of creating a new one. Use the task_id reported by a previous failed call to recover its output without paying for a regeneration.",
		},
		"video_id": map[string]interface{}{
			"type":        "string",
			"description": "Optional: re-attach using a known video_id (alternative to task_id)",
		},
	}
}

// resumeKeys are consumed by the adapter itself and must never be forwarded
// into the creation payload.
var resumeKeys = []string{"task_id", "video_id"}

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

// GenVideo calls the Agnes AI video generation API and returns the result.
// When req.Extra carries a task_id (and optionally a video_id) it skips
// creation and re-attaches to that existing task instead.
func (a *AgnesVideoAdapter) GenVideo(req VideoRequest) *VideoResult {
	modelUsed := defaultStr(req.Model, a.Model)

	if taskID, _ := req.Extra["task_id"].(string); taskID != "" {
		videoID, _ := req.Extra["video_id"].(string)
		logf("video resume: re-attaching to existing task_id=%s video_id=%s", taskID, orNone(videoID))
		return a.pollTask(taskID, videoID, modelUsed, req)
	}
	if videoID, _ := req.Extra["video_id"].(string); videoID != "" {
		logf("video resume: re-attaching to existing video_id=%s", videoID)
		return a.pollTask("", videoID, modelUsed, req)
	}

	numFrames := a.NumFrames

	// VideoRequest.Duration is a vendor-agnostic seconds value; Agnes controls
	// length via num_frames (must be 8n+1, <=441), so convert here instead of
	// sending a duration field the API ignores.
	if req.Duration > 0 {
		numFrames = fracToNumFrames(req.Duration, a.FrameRate)
	}

	// Per-request size override; keep in locals, never touch the shared
	// Width/Height (tools/call runs concurrently). 16:9/720p defaults apply
	// only when at least one size field is provided.
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
	for k, v := range req.Extra {
		payload[k] = v
	}
	for _, k := range resumeKeys {
		delete(payload, k)
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

	// Logged unconditionally: this is the only handle that can recover the
	// output if polling later goes wrong.
	logf("video submitted: task_id=%s video_id=%s", orNone(taskID), orNone(videoID))

	return a.pollTask(taskID, videoID, modelUsed, req)
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

// pollTask waits for an Agnes video task to finish.
//
// Both Agnes status endpoints are flaky under load: the status API is
// aggressively rate-limited (observed 429 on ~30% of requests at 4 concurrent
// pollers) and has been seen to answer 404 "task not found" for a task that
// the backend goes on to complete. Every HTTP failure is therefore treated as
// transient and retried against the alternate endpoint; the task is only
// abandoned after the whole grace window elapses with no usable response.
func (a *AgnesVideoAdapter) pollTask(taskID, videoID, modelUsed string, req VideoRequest) *VideoResult {
	host := strings.TrimSuffix(strings.TrimSuffix(a.BaseURL, "/"), "/v1/videos")
	host = strings.TrimSuffix(host, "/")

	client := &http.Client{Timeout: 30 * time.Second}
	tm := a.timing

	time.Sleep(tm.initialWait)

	startTime := time.Now()
	interval := tm.base
	lastStatus := ""
	lastProgress := 0.0
	lastProgressAt := time.Now()
	var firstFailureAt time.Time

	for {
		if elapsed := time.Since(startTime); elapsed >= tm.totalTimeout {
			return a.abandon(req, modelUsed, taskID, videoID,
				fmt.Errorf("timeout after %v (last status: %s)", tm.totalTimeout, orNone(lastStatus)))
		}

		st, ok, failure := a.probe(client, host, taskID, videoID)
		if !ok {
			if firstFailureAt.IsZero() {
				firstFailureAt = time.Now()
			}
			stuck := time.Since(firstFailureAt)
			if stuck >= tm.grace {
				return a.abandon(req, modelUsed, taskID, videoID,
					fmt.Errorf("status endpoints unreachable for %v (last failure: %s)", tm.grace, failure))
			}
			interval = growInterval(interval, tm.max)
			logf("video poll transient failure (%s), retrying in %v [%v/%v of grace window]",
				failure, interval, stuck.Truncate(time.Second), tm.grace)
			time.Sleep(interval)
			continue
		}
		firstFailureAt = time.Time{}

		// /v1/videos/<task_id> reveals the video_id even when creation did not.
		if videoID == "" && st.VideoID != "" {
			videoID = st.VideoID
			logf("video poll: discovered video_id=%s for task_id=%s", videoID, taskID)
		}

		if st.Status != lastStatus {
			lastStatus = st.Status
			logf("video status [%s]: %s (progress: %.0f%%)", orNone(taskID), orNone(st.Status), st.Progress)
		}

		// Adapt the polling interval to progress velocity: without a time-based
		// ETA, a fast-moving progress means we should poll sooner, a stalled one
		// means we should back off to avoid hammering the API.
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
				return a.abandon(req, modelUsed, taskID, videoID,
					fmt.Errorf("task completed but no video URL could be resolved"))
			}
			logf("video completed [%s]: %s", orNone(taskID), videoURL)
			return &VideoResult{
				Request:   req,
				ModelUsed: modelUsed,
				URLs:      []string{videoURL},
				FrameRate: st.FrameRate,
				Duration:  int(st.Seconds + 0.5),
			}

		case "failed", "error", "failure", "cancelled", "canceled":
			return &VideoResult{Request: req, ModelUsed: modelUsed,
				Error: fmt.Errorf("video generation failed: %s (task_id=%s)", defaultStr(st.ErrMsg, "unknown error"), orNone(taskID))}

		default:
			time.Sleep(interval)
		}
	}
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

// abandon reports a terminal polling failure with everything needed to recover
// the output out-of-band, since the backend task usually did succeed.
func (a *AgnesVideoAdapter) abandon(req VideoRequest, modelUsed, taskID, videoID string, cause error) *VideoResult {
	logf("VIDEO TASK ABANDONED — the task may still be running or already finished on Agnes.")
	logf("  task_id  = %s", orNone(taskID))
	logf("  video_id = %s", orNone(videoID))
	logf("  cause    = %v", cause)
	if videoID != "" {
		logf("  recover  : curl -H \"Authorization: Bearer $AGNES_AI_API_KEY\" \"https://api.agnes-ai.cn/agnesapi?video_id=%s\"", videoID)
	}
	if taskID != "" {
		logf("  or re-attach: call agnes_video_generateVideo again with task_id=%s", taskID)
	}

	return &VideoResult{
		Request:   req,
		ModelUsed: modelUsed,
		Error: fmt.Errorf("%w [task_id=%s video_id=%s] — the task may have completed on Agnes; "+
			"re-attach by calling this tool again with task_id set to recover the output",
			cause, orNone(taskID), orNone(videoID)),
	}
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

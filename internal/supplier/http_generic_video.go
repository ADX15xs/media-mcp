package supplier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"media-mcp/internal/config"
	"net/http"
	"time"
)

// HTTPGenericVideoAdapter is a catch-all VideoSupplier for video APIs that follow
// the standard async task pattern: POST to create a task, then GET /tasks/{id} to poll.
type HTTPGenericVideoAdapter struct {
	name          string
	BaseURL       string
	APIKey        string
	Model         string
	CustomHeaders map[string]string
	ExtraFields   map[string]interface{}
	Config        *config.SupplierConfig

	// Polling configuration (from Extra or defaults).
	pollInterval time.Duration

	// Response field names (configurable via Extra for non-standard APIs).
	taskIDField   string // JSON field name for task ID in creation response
	statusField   string // JSON field name for status in polling response
	videoURLField string // JSON field name for video URL in polling response
}

// NewHTTPGenericVideoAdapter creates a new HTTPGenericVideoAdapter from config.
func NewHTTPGenericVideoAdapter(name string, cfg *config.SupplierConfig) *HTTPGenericVideoAdapter {
	a := &HTTPGenericVideoAdapter{
		name:          name,
		BaseURL:       cfg.BaseURL,
		APIKey:        cfg.APIKey,
		Model:         cfg.Model,
		CustomHeaders: cfg.Headers,
		ExtraFields:   cfg.Extra,
		Config:        cfg,

		pollInterval: 5 * time.Second,

		taskIDField:   "task_id",
		statusField:   "status",
		videoURLField: "video_url",
	}

	// Override polling defaults from Extra.
	if v, ok := cfg.Extra["poll_interval_seconds"].(float64); ok && v > 0 {
		a.pollInterval = time.Duration(v) * time.Second
	}

	// Override JSON field names from Extra.
	if v, ok := cfg.Extra["task_id_field"].(string); ok && v != "" {
		a.taskIDField = v
	}
	if v, ok := cfg.Extra["status_field"].(string); ok && v != "" {
		a.statusField = v
	}
	if v, ok := cfg.Extra["video_url_field"].(string); ok && v != "" {
		a.videoURLField = v
	}

	return a
}

func (h *HTTPGenericVideoAdapter) Name() string {
	return h.name
}

// GenVideo calls the video generation API and returns the result.
// Supports both synchronous (returns video URL directly) and
// asynchronous (returns task_id, then poll) responses.
func (h *HTTPGenericVideoAdapter) GenVideo(req VideoRequest) *VideoResult {
	payload := map[string]interface{}{
		"prompt": req.Prompt,
	}
	if req.Model != "" {
		payload["model"] = req.Model
	} else if h.Model != "" {
		payload["model"] = h.Model
	}
	if req.Duration > 0 {
		payload["duration"] = req.Duration
	}
	if req.Style != "" {
		payload["style"] = req.Style
	}
	if req.Seed != nil {
		payload["seed"] = *req.Seed
	}

	for k, v := range h.ExtraFields {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}
	for k, v := range req.Extra {
		payload[k] = v
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return &VideoResult{Request: req, Error: fmt.Errorf("marshal request: %w", err)}
	}

	client := &http.Client{Timeout: 60 * time.Second}
	url := h.BaseURL
	if len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return &VideoResult{Request: req, Error: fmt.Errorf("create request: %w", err)}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	setAuthHeader(httpReq, h.Config, h.APIKey)
	for k, v := range h.CustomHeaders {
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &VideoResult{Request: req, Error: fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))}
	}

	// Parse creation response as generic JSON.
	createResp := make(map[string]interface{})
	if err := json.Unmarshal(body, &createResp); err != nil {
		return &VideoResult{Request: req, Error: fmt.Errorf("parse creation response: %w", err)}
	}

	// Check for error in response.
	if errMsg, ok := createResp["error"].(string); ok && errMsg != "" {
		return &VideoResult{Request: req, Error: fmt.Errorf("API error: %s", errMsg)}
	}
	if errObj, ok := createResp["error"].(map[string]interface{}); ok {
		if msg, ok := errObj["message"].(string); ok && msg != "" {
			return &VideoResult{Request: req, Error: fmt.Errorf("API error: %s", msg)}
		}
	}

	modelUsed := defaultStr(req.Model, h.Model)

	// Detect synchronous response (has video URL directly).
	if url, ok := createResp["url"].(string); ok && url != "" {
		return &VideoResult{
			Request:   req,
			ModelUsed: modelUsed,
			URLs:      []string{url},
		}
	}
	if data, ok := createResp["data"].([]interface{}); ok && len(data) > 0 {
		if item, ok := data[0].(map[string]interface{}); ok {
			if url, ok := item["url"].(string); ok && url != "" {
				return &VideoResult{
					Request:   req,
					ModelUsed: modelUsed,
					URLs:      []string{url},
				}
			}
		}
	}

	// Extract task ID.
	taskID := ""
	if v, ok := createResp[h.taskIDField].(string); ok && v != "" {
		taskID = v
	} else if v, ok := createResp["id"].(string); ok && v != "" {
		taskID = v
	}

	if taskID == "" {
		return &VideoResult{Request: req, Error: fmt.Errorf("no task id in response (looked for %q, id): %s", h.taskIDField, truncate(string(body), 200))}
	}

	return &VideoResult{
		Request:   req,
		ModelUsed: modelUsed,
		Status:    "working",
		TaskID:    taskID,
	}
}

// pollTask polls the task status endpoint until a terminal state or cap
// elapses. On cap it returns a working result so the caller retries.
func (h *HTTPGenericVideoAdapter) pollTask(taskID, modelUsed string, req VideoRequest, cap time.Duration) *VideoResult {
	baseURL := h.BaseURL
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}
	pollURL := fmt.Sprintf("%s/%s", baseURL, taskID)

	client := &http.Client{Timeout: 30 * time.Second}
	deadline := time.Now().Add(cap)
	lastStatus := ""

	for {
		if time.Now().After(deadline) {
			return &VideoResult{Request: req, ModelUsed: modelUsed, Status: "working", TaskID: taskID}
		}

		httpReq, err := http.NewRequest("GET", pollURL, nil)
		if err != nil {
			time.Sleep(h.pollInterval)
			continue
		}
		setAuthHeader(httpReq, h.Config, h.APIKey)
		for k, v := range h.CustomHeaders {
			httpReq.Header.Set(k, v)
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			time.Sleep(h.pollInterval)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			time.Sleep(h.pollInterval)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			logf("video poll [%s] transient HTTP %d, retrying", taskID, resp.StatusCode)
			time.Sleep(h.pollInterval)
			continue
		}

		statusResp := make(map[string]interface{})
		if err := json.Unmarshal(body, &statusResp); err != nil {
			return &VideoResult{Request: req, ModelUsed: modelUsed, Status: "failed", TaskID: taskID,
				Error: fmt.Errorf("parse status response: %w", err)}
		}

		status := ""
		if v, ok := statusResp[h.statusField].(string); ok {
			status = v
		}

		progress := 0.0
		if v, ok := statusResp["progress"].(float64); ok {
			progress = v
		}

		if status != lastStatus {
			lastStatus = status
			logf("video status [%s]: %s (progress: %.0f%%)", taskID, status, progress)
		}

		switch status {
		case "completed", "success", "succeeded":
			videoURL := ""
			if v, ok := statusResp[h.videoURLField].(string); ok && v != "" {
				videoURL = v
			} else if v, ok := statusResp["url"].(string); ok && v != "" {
				videoURL = v
			} else if v, ok := statusResp["video_url"].(string); ok && v != "" {
				videoURL = v
			} else if v, ok := statusResp["result_url"].(string); ok && v != "" {
				videoURL = v
			} else if data, ok := statusResp["data"].([]interface{}); ok && len(data) > 0 {
				if item, ok := data[0].(map[string]interface{}); ok {
					if u, ok := item["url"].(string); ok {
						videoURL = u
					}
				}
			}

			if videoURL == "" {
				return &VideoResult{Request: req, ModelUsed: modelUsed, Status: "failed", TaskID: taskID,
					Error: fmt.Errorf("no video URL in completed response (looked for %q, url, video_url, result_url)", h.videoURLField)}
			}

			frameRate := 0.0
			if v, ok := statusResp["frame_rate"].(float64); ok {
				frameRate = v
			}
			duration := 0
			if v, ok := statusResp["duration"].(float64); ok {
				duration = int(v)
			}

			return &VideoResult{
				Request:   req,
				ModelUsed: modelUsed,
				Status:    "completed",
				URLs:      []string{videoURL},
				FrameRate: frameRate,
				Duration:  duration,
			}

		case "failed", "error", "failure":
			errMsg := "unknown error"
			if v, ok := statusResp["error_message"].(string); ok && v != "" {
				errMsg = v
			} else if v, ok := statusResp["error"].(string); ok && v != "" {
				errMsg = v
			} else if errObj, ok := statusResp["error"].(map[string]interface{}); ok {
				if msg, ok := errObj["message"].(string); ok && msg != "" {
					errMsg = msg
				}
			}
			return &VideoResult{Request: req, ModelUsed: modelUsed, Status: "failed", TaskID: taskID,
				Error: fmt.Errorf("video generation failed: %s", errMsg)}

		default:
			time.Sleep(h.pollInterval)
		}
	}
}

// GetVideoResult polls an existing task for up to statusPollCap.
func (h *HTTPGenericVideoAdapter) GetVideoResult(taskID, videoID string) *VideoResult {
	if taskID == "" {
		return &VideoResult{Error: fmt.Errorf("task_id required")}
	}
	req := VideoRequest{Supplier: h.Name()}
	return h.pollTask(taskID, h.Model, req, statusPollCap)
}

// setAuthHeader sets the Authorization header based on the config's auth method.
func setAuthHeader(httpReq *http.Request, cfg *config.SupplierConfig, apiKey string) {
	switch {
	case cfg.AUTHMethod == "bearer" || cfg.AUTHMethod == "":
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	case cfg.AUTHMethod == "basic":
		httpReq.Header.Set("Authorization", "Basic "+apiKey)
	case cfg.CustomHeader != "":
		httpReq.Header.Set(cfg.CustomHeader, apiKey)
	default:
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

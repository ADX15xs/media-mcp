package supplier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"media-mcp/internal/config"
	"net/http"
	"os"
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
}

func NewAgnesVideoAdapter(cfg *config.SupplierConfig) *AgnesVideoAdapter {
	width := 1152
	height := 768
	numFrames := 121
	frameRate := 24

	if v, ok := cfg.Extra["width"].(float64); ok && v > 0 {
		width = int(v)
	}
	if v, ok := cfg.Extra["height"].(float64); ok && v > 0 {
		height = int(v)
	}
	if v, ok := cfg.Extra["num_frames"].(float64); ok && v > 0 {
		numFrames = int(v)
	}
	if v, ok := cfg.Extra["frame_rate"].(float64); ok && v > 0 {
		frameRate = int(v)
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
	}
}

func (a *AgnesVideoAdapter) Name() string {
	return "agnes_video"
}

// GenVideo calls the Agnes AI video generation API and returns the result.
func (a *AgnesVideoAdapter) GenVideo(req VideoRequest) *VideoResult {
	payload := map[string]interface{}{
		"model":      defaultStr(req.Model, a.Model),
		"prompt":     req.Prompt,
		"width":      a.Width,
		"height":     a.Height,
		"num_frames": a.NumFrames,
		"frame_rate": a.FrameRate,
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

	for k, v := range a.ExtraFields {
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
	url := a.BaseURL
	if len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &VideoResult{Request: req, Error: fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))}
	}

	var createResp struct {
		TaskID string `json:"task_id"`
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
		Raw map[string]interface{} `json:"-"`
	}
	if err := json.Unmarshal(body, &createResp); err != nil {
		return &VideoResult{Request: req, Error: fmt.Errorf("parse creation response: %w", err)}
	}
	createResp.Raw = make(map[string]interface{})
	json.Unmarshal(body, &createResp.Raw)

	taskID := createResp.TaskID
	if taskID == "" {
		taskID, _ = createResp.Raw["id"].(string)
	}
	if taskID == "" {
		return &VideoResult{Request: req, Error: fmt.Errorf("no task_id in response: %s", truncate(string(body), 200))}
	}

	if createResp.Error.Message != "" {
		return &VideoResult{Request: req, Error: fmt.Errorf("API error: %s", createResp.Error.Message)}
	}

	modelUsed := defaultStr(req.Model, a.Model)

	result := a.pollTask(taskID, modelUsed, req)
	if result.Error != nil {
		return result
	}
	return result
}

// pollTask polls the task status endpoint until completion or timeout.
func (a *AgnesVideoAdapter) pollTask(taskID, modelUsed string, req VideoRequest) *VideoResult {
	pollURL := fmt.Sprintf("%s/v1/videos/tasks/%s", a.BaseURL, taskID)
	if len(pollURL) > 0 && pollURL[len(pollURL)-1] == '/' {
		pollURL = pollURL[:len(pollURL)-1]
	}

	client := &http.Client{Timeout: 30 * time.Second}
	initialWait := 90 * time.Second
	pollInterval := 5 * time.Second
	maxTotalTimeout := 15 * time.Minute

	time.Sleep(initialWait)

	startTime := time.Now()
	lastStatus := ""

	for {
		elapsed := time.Since(startTime)
		if elapsed >= maxTotalTimeout {
			return &VideoResult{Request: req, ModelUsed: modelUsed, Error: fmt.Errorf("timeout after %v (task_id=%s)", maxTotalTimeout, taskID)}
		}

		httpReq, err := http.NewRequest("GET", pollURL, nil)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)
		for k, v := range a.CustomHeaders {
			httpReq.Header.Set(k, v)
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if resp.StatusCode == 404 {
				return &VideoResult{Request: req, ModelUsed: modelUsed, Error: fmt.Errorf("task %s not found", taskID)}
			}
			return &VideoResult{Request: req, ModelUsed: modelUsed, Error: fmt.Errorf("poll HTTP %d: %s", resp.StatusCode, truncate(string(body), 100))}
		}

		var statusResp struct {
			TaskID    string  `json:"task_id"`
			Status    string  `json:"status"`
			VideoURL  string  `json:"video_url"`
			FrameRate float64 `json:"frame_rate"`
			Duration  int     `json:"duration"`
			ErrorMsg  string  `json:"error_message"`
			Progress  float64 `json:"progress"`
			Raw       map[string]interface{}
		}
		if err := json.Unmarshal(body, &statusResp); err != nil {
			return &VideoResult{Request: req, ModelUsed: modelUsed, Error: fmt.Errorf("parse status response: %w", err)}
		}
		statusResp.Raw = make(map[string]interface{})
		json.Unmarshal(body, &statusResp.Raw)

		if statusResp.Status != lastStatus {
			lastStatus = statusResp.Status
			fmt.Fprintf(os.Stderr, "[media-mcp] video status [%s]: %s (progress: %.0f%%)\n", taskID, statusResp.Status, statusResp.Progress)
		}

		switch statusResp.Status {
		case "completed":
			if statusResp.VideoURL == "" {
				if url, ok := statusResp.Raw["url"].(string); ok {
					statusResp.VideoURL = url
				}
			}
			return &VideoResult{
				Request:   req,
				ModelUsed: modelUsed,
				URLs:      []string{statusResp.VideoURL},
				FrameRate: statusResp.FrameRate,
				Duration:  statusResp.Duration,
			}
		case "failed":
			msg := statusResp.ErrorMsg
			if msg == "" {
				msg = "unknown error"
			}
			return &VideoResult{Request: req, ModelUsed: modelUsed, Error: fmt.Errorf("video generation failed: %s", msg)}
		case "processing", "queued", "pending", "running":
			time.Sleep(pollInterval)
			continue
		default:
			time.Sleep(pollInterval)
			continue
		}
	}
}

func init() {
	RegisterVideo("agnes_video", func(cfg *config.SupplierConfig) (VideoSupplier, error) {
		return NewAgnesVideoAdapter(cfg), nil
	})
}

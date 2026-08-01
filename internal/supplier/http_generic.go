package supplier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"media-mcp/internal/config"
	"net/http"
)

// HTTPGenericAdapter is a catch-all adapter for suppliers that follow
// a standard OpenAI-compatible JSON response schema (data[].url | data[].b64_json).
type HTTPGenericAdapter struct {
	name          string
	SupplierType  string
	BaseURL       string
	APIKey        string
	Model         string
	Size          string
	N             int
	CustomHeaders map[string]string
	ExtraFields   map[string]interface{}
	Config        *config.SupplierConfig
}

func NewHTTPGenericAdapter(name string, cfg *config.SupplierConfig) *HTTPGenericAdapter {
	return &HTTPGenericAdapter{
		name:          name,
		SupplierType:  cfg.SupplierType,
		BaseURL:       cfg.BaseURL,
		APIKey:        cfg.APIKey,
		Model:         cfg.Model,
		Size:          cfg.Size,
		N:             intMax(cfg.Extra["n"], 1),
		CustomHeaders: cfg.Headers,
		ExtraFields:   cfg.Extra,
		Config:        cfg,
	}
}

func (h *HTTPGenericAdapter) Name() string {
	return h.name
}

func (h *HTTPGenericAdapter) GenImage(req ImageRequest) *ImageResult {
	payload := map[string]interface{}{
		"prompt": req.Prompt,
	}
	if req.Model != "" {
		payload["model"] = req.Model
	} else if h.Model != "" {
		payload["model"] = h.Model
	}
	if req.Size != "" {
		payload["size"] = req.Size
	} else if h.Size != "" {
		payload["size"] = h.Size
	}
	n := req.N
	if n <= 0 {
		n = h.N
	}
	payload["n"] = n

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
		return &ImageResult{Request: req, Error: fmt.Errorf("marshal request: %w", err)}
	}

	client := &http.Client{}
	url := h.BaseURL
	if len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return &ImageResult{Request: req, Error: fmt.Errorf("create request: %w", err)}
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Set auth based on config
	switch {
	case h.Config.AUTHMethod == "bearer" || h.Config.AUTHMethod == "":
		httpReq.Header.Set("Authorization", "Bearer "+h.APIKey)
	case h.Config.AUTHMethod == "basic":
		httpReq.Header.Set("Authorization", "Basic "+h.APIKey)
	case h.Config.CustomHeader != "":
		httpReq.Header.Set(h.Config.CustomHeader, h.APIKey)
	default:
		httpReq.Header.Set("Authorization", "Bearer "+h.APIKey)
	}

	for k, v := range h.CustomHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("HTTP request: %w", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("read response: %w", err)}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))}
	}

	var apiResp struct {
		Data []struct {
			URL     string `json:"url"`
			B64Json string `json:"b64_json"`
		} `json:"data"`
		Error string `json:"error"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("parse response: %w", err)}
	}

	if apiResp.Error != "" {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("API error: %s", apiResp.Error)}
	}

	result := &ImageResult{
		Request:   req,
		ModelUsed: req.Model,
	}

	for _, item := range apiResp.Data {
		if item.URL != "" {
			result.URLs = append(result.URLs, item.URL)
		} else if item.B64Json != "" {
			result.Base64 = append(result.Base64, item.B64Json)
		}
	}

	return result
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}

// intMax returns v if it's a positive number, otherwise def.
func intMax(a interface{}, def int) int {
	switch n := a.(type) {
	case float64:
		if n > 0 {
			return int(n)
		}
	case int:
		if n > 0 {
			return n
		}
	}
	return def
}

// defaultStr returns s if non-empty, otherwise def.
func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// maxInt calls intMax for backwards compatibility.
func maxInt(a interface{}, def int) int {
	return intMax(a, def)
}


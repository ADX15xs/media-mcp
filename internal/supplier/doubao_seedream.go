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

// DoubaoSeedreamAdapter implements ImageSupplier for Volcengine's Doubao Seedream API.
type DoubaoSeedreamAdapter struct {
	BaseURL       string
	APIKey        string
	Model         string
	Sizes         []string
	DefaultSize   string
	Models        []string
	OutputFormat  string // "png" or "jpeg"
	Watermark     *bool  // nil = let API decide
	MaxImages     int    // for sequential generation
	Config        *config.SupplierConfig
}

func NewDoubaoSeedreamAdapter(name string, cfg *config.SupplierConfig) *DoubaoSeedreamAdapter {
	outputFormat := "png"
	if v, ok := cfg.Extra["output_format"].(string); ok {
		outputFormat = v
	}
	watermark := false
	if v, ok := cfg.Extra["watermark"].(bool); ok {
		watermark = v
	}
	wp := &watermark

	sizes := []string{"2K"}
	for _, m := range cfg.Models {
		sizes = append(sizes, m)
	}

	models := []string{defaultStr(cfg.Model, "doubao-seedream-5-0-lite-260128")}
	if len(cfg.Models) > 0 {
		models = cfg.Models
	}
	if m, ok := cfg.Extra["models"].([]interface{}); ok {
		for _, mm := range m {
			if mstr, ok := mm.(string); ok {
				models = append(models, mstr)
			}
		}
	}

	return &DoubaoSeedreamAdapter{
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		Model:        cfg.Model,
		Sizes:        sizes,
		DefaultSize:  defaultStr(cfg.Size, "2K"),
		Models:       models,
		OutputFormat: outputFormat,
		Watermark:    wp,
		MaxImages:    intMax(cfg.Extra["max_images"], 1),
		Config:       cfg,
	}
}

func (d *DoubaoSeedreamAdapter) Name() string {
	// Use first model as name identifier, fallback to config key
	if len(d.Models) > 0 {
		return d.Models[0]
	}
	return "doubao_seedream"
}

func (d *DoubaoSeedreamAdapter) GenImage(req ImageRequest) *ImageResult {
	payload := map[string]interface{}{
		"model":   req.Model,
		"prompt":  req.Prompt,
		"size":    defaultStr(req.Size, d.DefaultSize),
		"output_format": d.OutputFormat,
		"response_format": "url",
	}

	if d.Watermark != nil {
		payload["watermark"] = *d.Watermark
	}

	// Support image-to-image via "image" field (single string per API docs)
	if img, ok := req.Extra["image"]; ok {
		switch v := img.(type) {
		case string:
			payload["image"] = v
		case []interface{}:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok {
					payload["image"] = s
				}
			}
		}
	}

	// Support negative prompt via extra_body
	if negPrompt, ok := req.Extra["negative_prompt"]; ok {
		payload["negative_prompt"] = negPrompt
	}

	// Support sequential multi-image generation
	if maxImgs, ok := req.Extra["max_images"]; ok {
		if n, ok := maxImgs.(float64); ok && n > 0 {
			payload["sequential_image_generation"] = "auto"
			payload["sequential_image_generation_options"] = map[string]interface{}{
				"max_images": int(n),
			}
		} else if maxImgStr, ok := maxImgs.(string); ok && maxImgStr == "auto" {
			payload["sequential_image_generation"] = "auto"
		}
	}

	// Enable streaming if configured
	if stream, ok := req.Extra["stream"].(bool); ok && stream {
		payload["stream"] = true
	}

	// Optimized prompt mode (only for 5.0 pro / 4.0)
	if mode, ok := req.Extra["optimize_mode"].(string); ok {
		payload["optimize_prompt_options"] = map[string]interface{}{
			"mode": mode,
		}
	}

	// Enable web search for 5.0 lite
	if ws, ok := req.Extra["web_search"].(bool); ok && ws {
		if payload["tools"] == nil {
			payload["tools"] = []map[string]interface{}{
				{"type": "web_search"},
			}
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return &ImageResult{Request: req, Error: fmt.Errorf("marshal request: %w", err)}
	}

	client := &http.Client{Timeout: 360 * time.Second}
	url := d.BaseURL
	if len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return &ImageResult{Request: req, Error: fmt.Errorf("create request: %w", err)}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+d.APIKey)

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
			URL string `json:"url"`
		} `json:"data"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		ErrorMsg string `json:"error_msg"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("parse response: %w", err)}
	}

	if apiResp.Error.Message != "" || apiResp.ErrorMsg != "" {
		msg := apiResp.Error.Message
		if msg == "" {
			msg = apiResp.ErrorMsg
		}
		code := fmt.Sprintf("[%d]", apiResp.Error.Code)
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("API error: %s %s", code, msg)}
	}

	result := &ImageResult{
		Request:   req,
		ModelUsed: req.Model,
	}

	for _, item := range apiResp.Data {
		if item.URL != "" {
			result.URLs = append(result.URLs, item.URL)
		}
	}

	return result
}

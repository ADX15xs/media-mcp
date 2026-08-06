package supplier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"media-mcp/internal/config"
	"net/http"
	"strings"
)

// DoubaoSeedreamAdapter implements ImageSupplier for Volcengine's Doubao Seedream API.
type DoubaoSeedreamAdapter struct {
	BaseURL      string
	APIKey       string
	Model        string
	Sizes        []string
	DefaultSize  string
	Models       []string
	ToolName     string // stable MCP tool identifier
	OutputFormat string // "png" or "jpeg"
	Watermark    *bool  // nil = let API decide
	MaxImages    int    // for sequential generation
	Config       *config.SupplierConfig
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

	// Tool name is stable: default to the config key so clients keep working
	// across model upgrades; allow an explicit override via extra.tool_name.
	toolName := name
	if name == "" {
		toolName = "doubao_seedream"
	}
	if v, ok := cfg.Extra["tool_name"].(string); ok && v != "" {
		toolName = v
	}

	return &DoubaoSeedreamAdapter{
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		Model:        cfg.Model,
		Sizes:        sizes,
		DefaultSize:  defaultStr(cfg.Size, "2K"),
		Models:       models,
		ToolName:     toolName,
		OutputFormat: outputFormat,
		Watermark:    wp,
		MaxImages:    intMax(cfg.Extra["max_images"], 1),
		Config:       cfg,
	}
}

func (d *DoubaoSeedreamAdapter) Name() string {
	return d.ToolName
}

// Capabilities explains the size semantics: both a resolution tier and an
// explicit WxH pixel value are supported, but must not be mixed. The tiers
// depend on the model (per the Volcengine docs); the configured model is
// resolved to its exact tiers when known.
func (d *DoubaoSeedreamAdapter) Capabilities() string {
	tiers := sizeTiersFor(d.Model)
	if tiers == "" {
		return "Size accepts a resolution tier (valid tiers depend on the model, see the Volcengine docs) or an explicit WxH pixel value; both are supported, do not mix them."
	}
	return "Size accepts a resolution tier (" + tiers + ") or an explicit WxH pixel value; both are supported, do not mix them."
}

// sizeTiersFor lists the resolution tiers of a Seedream model name per the
// Volcengine docs; "" means the name is unknown and the caller falls back.
func sizeTiersFor(model string) string {
	switch {
	case strings.Contains(model, "5.0-pro"), strings.Contains(model, "5.0 pro"):
		return "1K/1.5K/2K for 5.0 pro"
	case strings.Contains(model, "5.0-lite"), strings.Contains(model, "5.0 lite"):
		return "2K/3K/4K for 5.0 lite"
	case strings.Contains(model, "4.5"):
		return "2K/4K for 4.5"
	case strings.Contains(model, "4.0"):
		return "1K/2K/4K for 4.0"
	}
	return ""
}

func (d *DoubaoSeedreamAdapter) GenImage(req ImageRequest) *ImageResult {
	if req.Model == "" {
		req.Model = d.Model
	}
	payload := map[string]interface{}{
		"model":           req.Model,
		"prompt":          req.Prompt,
		"size":            defaultStr(req.Size, d.DefaultSize),
		"output_format":   d.OutputFormat,
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

	client := &http.Client{Timeout: imageHTTPTimeout}
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

func (d *DoubaoSeedreamAdapter) ExtraInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"negative_prompt": map[string]interface{}{
			"type":        "string",
			"description": "Optional: negative prompt",
		},
		"image": map[string]interface{}{
			"type":        "string",
			"description": "Optional: reference image (URL or base64 data URI) for image-to-image",
		},
		"max_images": map[string]interface{}{
			"type":        "number",
			"description": "Optional: generate up to N images sequentially (reference image count + generated images must be <= 15)",
		},
		"stream": map[string]interface{}{
			"type":        "boolean",
			"description": "Optional: enable streaming response",
		},
		"optimize_mode": map[string]interface{}{
			"type":        "string",
			"description": "Optional: prompt optimization mode. standard (default, all models) or fast (not supported by 5.0 lite / 4.5)",
			"enum":        []string{"standard", "fast"},
		},
		"web_search": map[string]interface{}{
			"type":        "boolean",
			"description": "Optional: enable web search grounding (5.0 lite)",
		},
	}
}

func init() {
	RegisterImage("doubao_seedream", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return NewDoubaoSeedreamAdapter("doubao_seedream", cfg), nil
	})
}

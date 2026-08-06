package supplier

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"media-mcp/internal/config"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AgnesAIAdapter implements ImageSupplier for Agnes AI platform.
// Handles both Image 2.0 Flash and 2.1 Flash with their unique extra_body structure.
type AgnesAIAdapter struct {
	BaseURL         string
	APIKey          string
	Model           string
	Size            string
	N               int
	CustomHeaders   map[string]string
	ExtraFields     map[string]interface{}
	Config          *config.SupplierConfig
	SupportedRatios []string
}

func NewAgnesAIAdapter(cfg *config.SupplierConfig) *AgnesAIAdapter {
	ratios := []string{"1:1"}
	if raw, ok := cfg.Extra["supported_ratios"].([]interface{}); ok && len(raw) > 0 {
		ratios = ratios[:0]
		for _, r := range raw {
			if rs, ok := r.(string); ok && rs != "" {
				ratios = append(ratios, rs)
			}
		}
	}
	seen := make(map[string]bool, len(ratios))
	unique := ratios[:0]
	for _, r := range ratios {
		if !seen[r] {
			seen[r] = true
			unique = append(unique, r)
		}
	}

	return &AgnesAIAdapter{
		BaseURL:         cfg.BaseURL,
		APIKey:          cfg.APIKey,
		Model:           defaultStr(cfg.Model, "agnes-image-2.1-flash"),
		Size:            defaultStr(cfg.Size, "1024x768"),
		N:               maxInt(cfg.Extra["n"], 1),
		CustomHeaders:   cfg.Headers,
		ExtraFields:     cfg.Extra,
		Config:          cfg,
		SupportedRatios: unique,
	}
}

func (a *AgnesAIAdapter) Name() string {
	return "agnes_ai"
}

// Capabilities describes the size/ratio normalization rules agents should know
// before picking parameters: the API rounds sizes up to standard tiers.
func (a *AgnesAIAdapter) Capabilities() string {
	return "Size note: the API normalizes requested sizes to standard tiers " +
		"(e.g. 1024x768 -> 1152x864 for 4:3 @1K). " +
		"Use size + ratio for predictable output; supported ratios: " +
		strings.Join(a.SupportedRatios, ", ") + "."
}

func (a *AgnesAIAdapter) ExtraInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"ratio": map[string]interface{}{
			"type":        "string",
			"description": "Optional: output aspect ratio for Image 2.1 Flash. Supported: " + strings.Join(a.SupportedRatios, ", ") + ".",
			"enum":        a.SupportedRatios,
		},
		"image": map[string]interface{}{
			"type":        "string",
			"description": "Optional: reference image (URL or base64 data URI) for image-to-image",
		},
		"return_base64": map[string]interface{}{
			"type":        "boolean",
			"description": "Optional: return base64-encoded image data instead of a URL",
		},
	}
}

func (a *AgnesAIAdapter) GenImage(req ImageRequest) *ImageResult {
	if req.Model == "" {
		req.Model = a.Model
	}
	if req.Size == "" {
		req.Size = a.Size
	}
	n := req.N
	if n <= 0 {
		n = a.N
	}

	// Build payload - Agnes AI uses extra_body for response_format and image inputs
	payload := map[string]interface{}{
		"model":  req.Model,
		"prompt": req.Prompt,
		"size":   req.Size,
	}

	// Handle ratio for 2.1 Flash
	if ratio, ok := req.Extra["ratio"].(string); ok && ratio != "" {
		payload["ratio"] = ratio
	}

	// Determine output format
	responseFormat := "url"
	if req.Extra["return_base64"] == true {
		responseFormat = "b64_json"
	}

	// Build extra_body for Agnes AI specific fields
	extraBody := map[string]interface{}{
		"response_format": responseFormat,
	}

	// Handle image input (img2img / multi-image)
	if img, ok := req.Extra["image"]; ok {
		extraBody["image"] = img
	}

	// Add any extra fields from the adapter config
	for k, v := range a.ExtraFields {
		if k != "supported_ratios" { // skip config-only fields
			extraBody[k] = v
		}
	}

	// Add request-level extras that belong in extra_body
	for k, v := range req.Extra {
		switch k {
		case "image", "ratio", "return_base64":
			// already handled above or will be in extra_body below
		default:
			extraBody[k] = v
		}
	}

	payload["extra_body"] = extraBody

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return &ImageResult{Request: req, Error: fmt.Errorf("marshal request: %w", err)}
	}

	client := &http.Client{Timeout: imageHTTPTimeout}
	url := a.BaseURL
	if len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return &ImageResult{Request: req, Error: fmt.Errorf("create request: %w", err)}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)

	for k, v := range a.CustomHeaders {
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
		Created int64 `json:"created"`
		Data    []struct {
			URL           string `json:"url"`
			B64Json       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("parse response: %w", err)}
	}

	if apiResp.Error.Message != "" {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("API error: %s", apiResp.Error.Message)}
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

	// If base64 returned, save to temp file as PNG
	if len(result.Base64) > 0 {
		for i, b64 := range result.Base64 {
			data, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				result.Error = fmt.Errorf("decode base64 index %d: %w", i, err)
				continue
			}

			tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("agnes_img_%d.png", time.Now().UnixNano()))
			if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
				result.Error = fmt.Errorf("save file %d: %w", i, err)
				continue
			}
			result.URLs = append(result.URLs, tmpFile)
		}
	}

	return result
}

func init() {
	RegisterImage("agnes_ai", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return NewAgnesAIAdapter(cfg), nil
	})
}

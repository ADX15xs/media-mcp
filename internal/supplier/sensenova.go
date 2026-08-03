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
	"time"
)

// SenseNovaAdapter implements ImageSupplier for the SenseNova platform.
type SenseNovaAdapter struct {
	BaseURL       string
	APIKey        string
	Model         string
	Size          string
	N             int
	CustomHeaders map[string]string
	ExtraFields   map[string]interface{}
	Config        *config.SupplierConfig // original config for reference
}

func init() {
	RegisterImage("senseNova", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return &SenseNovaAdapter{
			BaseURL:       cfg.BaseURL,
			APIKey:        cfg.APIKey,
			Model:         defaultStr(cfg.Model, "sensenova-u1-fast"),
			Size:          defaultStr(cfg.Size, "2752x1536"),
			N:             intMax(cfg.Extra["n"], 1),
			CustomHeaders: cfg.Headers,
			ExtraFields:   cfg.Extra,
			Config:        cfg,
		}, nil
	})
}

// Name returns the unique identifier for this supplier.
func (s *SenseNovaAdapter) Name() string {
	return "senseNova"
}

// GenImage calls the SenseNova image generation API and returns the result.
func (s *SenseNovaAdapter) GenImage(req ImageRequest) *ImageResult {
	if req.Model == "" {
		req.Model = s.Model
	}
	if req.Size == "" {
		req.Size = s.Size
	}
	if req.N <= 0 {
		req.N = s.N
	}

	payload := make(map[string]interface{})
	payload["model"] = req.Model
	payload["prompt"] = req.Prompt
	payload["size"] = req.Size
	payload["n"] = req.N

	// Add any custom fields from the request.
	for k, v := range req.Extra {
		payload[k] = v
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return &ImageResult{Request: req, Error: fmt.Errorf("marshal request: %w", err)}
	}

	client := &http.Client{Timeout: 180 * time.Second}
	url := s.BaseURL
	if len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}

	reqHttp, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return &ImageResult{Request: req, Error: fmt.Errorf("create request: %w", err)}
	}

	reqHttp.Header.Set("Content-Type", "application/json")
	reqHttp.Header.Set("Authorization", "Bearer "+s.APIKey)

	for k, v := range s.CustomHeaders {
		reqHttp.Header.Set(k, v)
	}

	resp, err := client.Do(reqHttp)
	if err != nil {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("HTTP request: %w", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("read response: %w", err)}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))}
	}

	var apiResp struct {
		Data []struct {
			URL     string `json:"url"`
			B64Json string `json:"b64_json"`
		} `json:"data"`
		Error string `json:"error"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("parse response JSON: %w", err)}
	}

	if apiResp.Error != "" {
		return &ImageResult{Request: req, ModelUsed: req.Model, Error: fmt.Errorf("API error: %s", apiResp.Error)}
	}

	result := &ImageResult{
		Request:   req,
		ModelUsed: req.Model,
		Error:     nil,
	}

	for _, item := range apiResp.Data {
		if item.URL != "" {
			result.URLs = append(result.URLs, item.URL)
		} else if item.B64Json != "" {
			result.Base64 = append(result.Base64, item.B64Json)
		}
	}

	// If base64 returned, try to save to a temp file as PNG.
	if len(result.Base64) > 0 {
		for i, b64 := range result.Base64 {
			data, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				result.Error = fmt.Errorf("decode base64 index %d: %w", i, err)
				continue
			}

			tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("sensenova_img_%d.png", time.Now().UnixNano()))
			if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
				result.Error = fmt.Errorf("save file %d: %w", i, err)
				continue
			}
			result.URLs = append(result.URLs, tmpFile)
		}
	}

	return result
}

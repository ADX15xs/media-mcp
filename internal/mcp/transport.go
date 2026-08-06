package mcp

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"media-mcp/internal/config"
	"media-mcp/internal/supplier"
	"os"
	"strings"
	"sync"
)

// Server implements the MCP stdio JSON-RPC transport.
type Server struct {
	imageSuppliers []supplier.ImageSupplier
	videoSuppliers []supplier.VideoSupplier
	cfg            *config.GlobalConfig
	mu             sync.Mutex
}

func NewServer(cfg *config.GlobalConfig) *Server {
	return &Server{cfg: cfg}
}

// RegisterSuppliers adds image and video suppliers to the server.
func (s *Server) RegisterSuppliers(image []supplier.ImageSupplier, video []supplier.VideoSupplier) {
	s.imageSuppliers = image
	s.videoSuppliers = video
}

// Start begins reading from stdin and writing to stdout via JSON-RPC.
func (s *Server) Start() error {
	reader := bufio.NewReader(os.Stdin)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("read line: %w", err)
		}

		line = trimTrailing(line)

		if line == "" {
			continue
		}

		raw := []byte(line)

		var msg jsonMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			s.sendError(nil, -32700, "Parse error")
			continue
		}

		switch msg.Method {
		case "initialize":
			s.handleInitialize(msg)
		case "initialized":
		case "notifications/initialized":
		case "notifications/cancelled":
		case "tools/list":
			s.handleToolsList(msg)
		case "tools/call":
			go s.handleToolCall(msg)
		case "ping":
			s.sendResult(msg.ID, map[string]interface{}{})
		default:
			s.sendError(msg.ID, -32601, "Method not found: "+msg.Method)
		}
	}

	return nil
}

func (s *Server) handleInitialize(msg jsonMessage) {
	result := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "media-mcp-server",
			"version": "0.2.0",
		},
	}
	s.sendResult(msg.ID, result)
	s.sendNotification("notifications/initialized", nil)
}

func (s *Server) handleToolsList(msg jsonMessage) {
	tools := make([]map[string]interface{}, 0, len(s.imageSuppliers)+len(s.videoSuppliers))

	for _, sup := range s.imageSuppliers {
		name := fmt.Sprintf("%s_generateImage", sup.Name())
		desc := fmt.Sprintf("Generate an image using the %s supplier", sup.Name())
		if c, ok := sup.(supplier.CapabilityProvider); ok {
			desc += ". " + c.Capabilities()
		}
		tools = append(tools, map[string]interface{}{
			"name":        name,
			"description": desc,
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "The prompt to generate the image",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional: model name",
					},
					"size": map[string]interface{}{
						"type":        "string",
						"description": "Optional: image size (e.g. 2752x1536)",
					},
					"n": map[string]interface{}{
						"type":        "number",
						"description": "Optional: number of images to generate",
					},
					"negative_prompt": map[string]interface{}{
						"type":        "string",
						"description": "Optional: negative prompt",
					},
				},
				"required": []string{"prompt"},
			},
		})
	}

	for _, sup := range s.videoSuppliers {
		name := fmt.Sprintf("%s_generateVideo", sup.Name())
		desc := fmt.Sprintf("Generate a video using the %s supplier", sup.Name())
		if c, ok := sup.(supplier.CapabilityProvider); ok {
			desc += ". " + c.Capabilities()
		}
		tools = append(tools, map[string]interface{}{
			"name":        name,
			"description": desc,
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "The prompt to generate the video",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional: model name",
					},
					"duration": map[string]interface{}{
						"type":        "number",
						"description": "Optional: video duration in seconds",
					},
					"style": map[string]interface{}{
						"type":        "string",
						"description": "Optional: style hint",
					},
					"seed": map[string]interface{}{
						"type":        "number",
						"description": "Optional: reproducibility seed",
					},
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional: re-attach to an existing task instead of creating a new one. Use the task_id reported by a previous failed call to recover its output without paying for a regeneration.",
					},
					"video_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional: re-attach using a known video_id (alternative to task_id)",
					},
				},
				"required": []string{"prompt"},
			},
		})
	}

	s.sendResult(msg.ID, map[string]interface{}{
		"tools": tools,
	})
}

func (s *Server) handleToolCall(msg jsonMessage) {
	type toolCallRequest struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}

	var req toolCallRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		s.sendError(msg.ID, -32602, "Invalid params: "+err.Error())
		return
	}

	for _, sup := range s.imageSuppliers {
		prefix := sup.Name() + "_generateImage"
		if req.Name == prefix {
			imageReq := supplier.ImageRequest{
				Prompt:         getString(req.Arguments, "prompt"),
				NegativePrompt: getString(req.Arguments, "negative_prompt"),
				Model:          getString(req.Arguments, "model"),
				Size:           getString(req.Arguments, "size"),
				N:              getInt(req.Arguments, "n"),
				Supplier:       sup.Name(),
			}
			result := sup.GenImage(imageReq)
			s.sendImageResult(msg.ID, result)
			return
		}
	}

	for _, sup := range s.videoSuppliers {
		prefix := sup.Name() + "_generateVideo"
		if req.Name == prefix {
			videoReq := supplier.VideoRequest{
				Prompt:   getString(req.Arguments, "prompt"),
				Model:    getString(req.Arguments, "model"),
				Duration: getInt(req.Arguments, "duration"),
				Style:    getString(req.Arguments, "style"),
				Supplier: sup.Name(),
			}
			if seed, ok := req.Arguments["seed"]; ok {
				if n, ok := seed.(float64); ok {
					seedInt := int(n)
					videoReq.Seed = &seedInt
				}
			}
			for _, k := range []string{"task_id", "video_id"} {
				if v := getString(req.Arguments, k); v != "" {
					if videoReq.Extra == nil {
						videoReq.Extra = map[string]interface{}{}
					}
					videoReq.Extra[k] = v
				}
			}
			result := sup.GenVideo(videoReq)
			s.sendVideoResult(msg.ID, result)
			return
		}
	}

	s.sendError(msg.ID, -32603, "Internal error: supplier not found for tool "+req.Name)
}

func (s *Server) sendImageResult(id *jsonNumber, result *supplier.ImageResult) {
	var content []map[string]interface{}
	if result.Error != nil {
		content = []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Error generating image for %s: %v\nURLs: %v\nBase64 count: %d",
					result.Request.Supplier, result.Error, result.URLs, len(result.Base64)),
			},
		}
	} else {
		textParts := []string{}
		if len(result.URLs) > 0 {
			textParts = append(textParts, fmt.Sprintf("Generated images (%d total):", len(result.URLs)))
			for i, url := range result.URLs {
				textParts = append(textParts, fmt.Sprintf("  %d: %s", i+1, url))
			}
		} else {
			textParts = append(textParts, "No images generated.")
		}
		textParts = append(textParts, fmt.Sprintf("Model used: %s", result.ModelUsed))

		content = []map[string]interface{}{
			{
				"type": "text",
				"text": joinStrings(textParts, "\n"),
			},
		}

		for _, url := range result.URLs {
			if strings.HasPrefix(url, "http") {
				content = append(content, map[string]interface{}{
					"type": "resource_link",
					"name": urlName(url),
					"uri":  url,
				})
			} else {
				fileData, err := os.ReadFile(url)
				if err != nil {
					continue
				}
				content = append(content, map[string]interface{}{
					"type":     "image",
					"data":     base64.StdEncoding.EncodeToString(fileData),
					"mimeType": mimeTypeFromURL(url),
				})
			}
		}
	}

	s.sendResult(id, map[string]interface{}{
		"content": content,
		"isError": result.Error != nil,
	})
}

func (s *Server) sendVideoResult(id *jsonNumber, result *supplier.VideoResult) {
	var content []map[string]interface{}
	if result.Error != nil {
		content = []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Error generating video for %s: %v",
					result.Request.Supplier, result.Error),
			},
		}
	} else {
		textParts := []string{}
		if len(result.URLs) > 0 {
			textParts = append(textParts, fmt.Sprintf("Generated videos (%d total):", len(result.URLs)))
			for i, url := range result.URLs {
				textParts = append(textParts, fmt.Sprintf("  %d: %s", i+1, url))
			}
		} else {
			textParts = append(textParts, "No videos generated.")
		}
		textParts = append(textParts, fmt.Sprintf("Model used: %s", result.ModelUsed))
		if result.Duration > 0 {
			textParts = append(textParts, fmt.Sprintf("Duration: %d seconds", result.Duration))
		}
		if result.FrameRate > 0 {
			textParts = append(textParts, fmt.Sprintf("Frame rate: %.1f fps", result.FrameRate))
		}

		content = []map[string]interface{}{
			{
				"type": "text",
				"text": joinStrings(textParts, "\n"),
			},
		}

		for _, url := range result.URLs {
			if strings.HasPrefix(url, "http") {
				content = append(content, map[string]interface{}{
					"type": "resource_link",
					"name": urlName(url),
					"uri":  url,
				})
			}
		}
	}

	s.sendResult(id, map[string]interface{}{
		"content": content,
		"isError": result.Error != nil,
	})
}

func (s *Server) sendResult(id *jsonNumber, result interface{}) {
	resp := jsonResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.write(resp)
}

func (s *Server) sendError(id *jsonNumber, code int, message string) {
	resp := jsonResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	s.write(resp)
}

func (s *Server) sendNotification(method string, params interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	s.write(msg)
}

func (s *Server) write(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal response: %v\n", err)
		return
	}
	// Use newline-delimited JSON (NDJSON) format — one JSON message per line.
	// This is the MCP standard for stdio transport.
	s.mu.Lock()
	defer s.mu.Unlock()
	data = append(data, '\n')
	os.Stdout.Write(data)
	os.Stdout.Sync()
}

// --- helpers ---

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

func joinStrings(ss []string, sep string) string {
	var buf bytes.Buffer
	for i, s := range ss {
		if i > 0 {
			buf.WriteString(sep)
		}
		buf.WriteString(s)
	}
	return buf.String()
}

// trimTrailing removes trailing \r, \n, or \r\n from s.
func trimTrailing(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// mimeTypeFromURL returns a MIME type based on the URL's file extension.
// Handles query strings, fragments, and uppercase extensions.
func mimeTypeFromURL(rawURL string) string {
	u := rawURL
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	u = strings.ToLower(u)

	switch {
	case strings.HasSuffix(u, ".png"):
		return "image/png"
	case strings.HasSuffix(u, ".jpg"), strings.HasSuffix(u, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(u, ".webp"):
		return "image/webp"
	case strings.HasSuffix(u, ".gif"):
		return "image/gif"
	case strings.HasSuffix(u, ".mp4"):
		return "video/mp4"
	case strings.HasSuffix(u, ".webm"):
		return "video/webm"
	case strings.HasSuffix(u, ".avi"):
		return "video/x-msvideo"
	case strings.HasSuffix(u, ".mov"):
		return "video/quicktime"
	default:
		return "application/octet-stream"
	}
}

// urlName extracts a display name from a URL's last path segment.
func urlName(rawURL string) string {
	u := rawURL
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	if u == "" {
		return rawURL
	}
	return u
}

// --- JSON-RPC types ---

type jsonMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method,omitempty"`
	ID      *jsonNumber     `json:"id,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      *jsonNumber `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

type jsonNumber struct {
	Num  int
	Seen bool
}

func (j *jsonNumber) MarshalJSON() ([]byte, error) {
	if !j.Seen {
		return []byte("null"), nil
	}
	return []byte(fmt.Sprintf("%d", j.Num)), nil
}

func (j *jsonNumber) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	j.Num = n
	j.Seen = true
	return nil
}

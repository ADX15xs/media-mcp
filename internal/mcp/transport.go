package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"media-mcp/internal/config"
	"media-mcp/internal/supplier"
	"os"
	"sync"
)

// Server implements the MCP stdio JSON-RPC transport.
type Server struct {
	suppliers []supplier.ImageSupplier
	cfg       *config.GlobalConfig
	mu        sync.Mutex // protects map mutations during boot-up init
}

func NewServer(cfg *config.GlobalConfig) *Server {
	return &Server{cfg: cfg}
}

// RegisterSuppliers adds image suppliers to the server.
func (s *Server) RegisterSuppliers(list []supplier.ImageSupplier) {
	s.suppliers = list
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

		var msg jsonMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			s.sendError(nil, -32700, "Parse error")
			continue
		}

		switch msg.Method {
		case "initialize":
			s.handleInitialize(msg)
		case "initialized":
			// Client-side initialized acknowledgment; no-op.
		case "notifications/initialized":
			// Same, no-op on server side.
		case "tools/list":
			s.handleToolsList(msg)
		case "tools/call":
			s.handleToolCall(msg)
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
			"version": "0.1.0",
		},
	}
	s.sendResult(msg.ID, result)

	// Notify initialized
	s.sendNotification("notifications/initialized", nil)
}

func (s *Server) handleToolsList(msg jsonMessage) {
	tools := make([]map[string]interface{}, 0, len(s.suppliers)*2)

	for _, sup := range s.suppliers {
		name := fmt.Sprintf("%s_generateImage", sup.Name())
		tools = append(tools, map[string]interface{}{
			"name":        name,
			"description": fmt.Sprintf("Generate an image using the %s supplier", sup.Name()),
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

	imageReq := supplier.ImageRequest{
		Prompt:     getString(req.Arguments, "prompt"),
		NegativePrompt: getString(req.Arguments, "negative_prompt"),
		Model:      getString(req.Arguments, "model"),
		Size:       getString(req.Arguments, "size"),
		N:          getInt(req.Arguments, "n"),
	}

	// Find matching supplier by checking if tool name starts with supplier name + "_generateImage"
	for _, sup := range s.suppliers {
		prefix := sup.Name() + "_generateImage"
		if req.Name == prefix {
			imageReq.Supplier = sup.Name()
			result := sup.GenImage(imageReq)
			
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

				// Add image content blocks for URLs (file:// if local)
				for _, url := range result.URLs {
					if len(url) > 4 && (url[:4] == "http" || url[:4] == "https") {
						// Remote URL — attach as a data URI
						content = append(content, map[string]interface{}{
							"type":     "image",
							"data":     url,
							"mimeType": "image/png",
						})
					} else {
						// Local file — read it and embed as base64
						fileData, err := os.ReadFile(url)
						if err != nil {
							continue
						}
						content = append(content, map[string]interface{}{
							"type":     "image",
							"data":     string(fileData),
							"mimeType": "image/png",
						})
					}
				}
			}

			s.sendResult(msg.ID, map[string]interface{}{
				"content": content,
				"isError": result.Error != nil,
			})
			return
		}
	}

	s.sendError(msg.ID, -32603, "Internal error: supplier not found for tool "+req.Name)
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
	// MCP over stdio uses Content-Length header format
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	os.Stdout.WriteString(header)
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

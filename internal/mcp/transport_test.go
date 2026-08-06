package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"media-mcp/internal/config"
	"media-mcp/internal/supplier"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock suppliers
// ---------------------------------------------------------------------------

type mockImageSupplier struct {
	name     string
	genImage func(req supplier.ImageRequest) *supplier.ImageResult
}

func (m *mockImageSupplier) Name() string { return m.name }

func (m *mockImageSupplier) GenImage(req supplier.ImageRequest) *supplier.ImageResult {
	if m.genImage != nil {
		return m.genImage(req)
	}
	return &supplier.ImageResult{
		URLs:      []string{"https://example.com/img.png"},
		ModelUsed: "mock-model",
		Request:   req,
	}
}

type mockVideoSupplier struct {
	name     string
	genVideo func(req supplier.VideoRequest) *supplier.VideoResult
}

func (m *mockVideoSupplier) Name() string { return m.name }

func (m *mockVideoSupplier) GenVideo(req supplier.VideoRequest) *supplier.VideoResult {
	if m.genVideo != nil {
		return m.genVideo(req)
	}
	return &supplier.VideoResult{
		URLs:      []string{"https://example.com/vid.mp4"},
		ModelUsed: "mock-video-model",
		Request:   req,
	}
}

type mockVideoStatusSupplier struct {
	mockVideoSupplier
	getResult func(taskID, videoID string) *supplier.VideoResult
}

func (m *mockVideoStatusSupplier) GetVideoResult(taskID, videoID string) *supplier.VideoResult {
	if m.getResult != nil {
		return m.getResult(taskID, videoID)
	}
	return &supplier.VideoResult{Status: "working", TaskID: taskID, ModelUsed: "mock", Request: supplier.VideoRequest{Supplier: m.name}}
}

type mockExtImageSupplier struct {
	mockImageSupplier
	extraSchema map[string]interface{}
}

func (m *mockExtImageSupplier) ExtraInputSchema() map[string]interface{} {
	return m.extraSchema
}

// ---------------------------------------------------------------------------
// Test harness — replaces os.Stdin / os.Stdout with pipes
// ---------------------------------------------------------------------------

type mcpTestHarness struct {
	t            *testing.T
	stdinPipe    *os.File
	stdoutPipe   *os.File
	server       *Server
	serverErr    chan error
	cancel       func()
}

func newMCPTestHarness(t *testing.T, imageSuppliers []supplier.ImageSupplier, videoSuppliers []supplier.VideoSupplier) *mcpTestHarness {
	t.Helper()

	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = stdinReader
	os.Stdout = stdoutWriter

	cfg := &config.GlobalConfig{
		DefaultSupplier: "mock",
		Suppliers: map[string]config.SupplierConfig{
			"mock": {Enabled: true, SupplierType: "image", APIKey: "test", BaseURL: "https://example.com"},
		},
	}

	server := NewServer(cfg)
	server.RegisterSuppliers(imageSuppliers, videoSuppliers)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start()
	}()
	return &mcpTestHarness{
		t:            t,
		stdinPipe:    stdinWriter,
		stdoutPipe:   stdoutReader,
		server:       server,
		serverErr:    serverErr,
		cancel: func() {
			stdinWriter.Close()
			stdoutWriter.Close()
			os.Stdin = origStdin
			os.Stdout = origStdout
		},
	}
}

func (h *mcpTestHarness) sendRequest(method string, id int, params interface{}) {
	h.t.Helper()
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	data, err := json.Marshal(msg)
	if err != nil {
		h.t.Fatalf("marshal request: %v", err)
	}
	data = append(data, '\n')
	if _, err := h.stdinPipe.Write(data); err != nil {
		h.t.Fatalf("write request: %v", err)
	}
}



func (h *mcpTestHarness) sendNotification(method string, params interface{}) {
	h.t.Helper()
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	data, err := json.Marshal(msg)
	if err != nil {
		h.t.Fatalf("marshal notification: %v", err)
	}
	data = append(data, '\n')
	if _, err := h.stdinPipe.Write(data); err != nil {
		h.t.Fatalf("write notification: %v", err)
	}
}

func (h *mcpTestHarness) readResponse() map[string]interface{} {
	h.t.Helper()
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		var buf bytes.Buffer
		tmp := make([]byte, 1)
		for {
			_, err := h.stdoutPipe.Read(tmp)
			if err != nil {
				ch <- result{nil, err}
				return
			}
			buf.WriteByte(tmp[0])
			if tmp[0] == '\n' {
				ch <- result{buf.Bytes(), nil}
				return
			}
		}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			h.t.Fatalf("read response: %v", r.err)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(r.data, &resp); err != nil {
			h.t.Fatalf("unmarshal response %q: %v", string(r.data), err)
		}
		return resp
	case <-time.After(5 * time.Second):
		h.t.Fatal("timeout reading response")
		return nil
	}
}

func (h *mcpTestHarness) close() {
	h.cancel()
}

// ---------------------------------------------------------------------------
// MCP Protocol Compliance Tests
// ---------------------------------------------------------------------------

func TestInitialize(t *testing.T) {
	imgSup := &mockImageSupplier{name: "mock"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "test-client",
			"version": "1.0.0",
		},
	})

	resp := h.readResponse()
	if resp["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", resp["jsonrpc"])
	}
	id, _ := resp["id"].(float64)
	if id != 1 {
		t.Errorf("id = %v, want 1", id)
	}
	if resp["error"] != nil {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %v", resp["result"])
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}
	info, _ := result["serverInfo"].(map[string]interface{})
	if info == nil {
		t.Fatal("missing serverInfo")
	}
	if info["name"] != "media-mcp-server" {
		t.Errorf("serverInfo.name = %v", info["name"])
	}
	if info["version"] != "0.2.0" {
		t.Errorf("serverInfo.version = %v", info["version"])
	}
	caps, _ := result["capabilities"].(map[string]interface{})
	if caps == nil {
		t.Fatal("missing capabilities")
	}
	if caps["tools"] == nil {
		t.Error("capabilities missing tools")
	}
}

func TestInitializeSendsInitializedNotification(t *testing.T) {
	imgSup := &mockImageSupplier{name: "mock"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	resp1 := h.readResponse()
	if resp1["error"] != nil {
		t.Fatalf("initialize error: %v", resp1["error"])
	}

	resp2 := h.readResponse()
	if resp2["id"] != nil {
		t.Errorf("notification should not have id, got %v", resp2["id"])
	}
	if resp2["method"] != "notifications/initialized" {
		t.Errorf("notification method = %v, want notifications/initialized", resp2["method"])
	}
}

func TestToolsList(t *testing.T) {
	imgSup := &mockImageSupplier{name: "testImg"}
	vidSup := &mockVideoSupplier{name: "testVid"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, []supplier.VideoSupplier{vidSup})
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/list", 2, nil)
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("tools/list error: %v", resp["error"])
	}
	result, _ := resp["result"].(map[string]interface{})
	tools, _ := result["tools"].([]interface{})
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	tool0 := tools[0].(map[string]interface{})
	if tool0["name"] != "testImg_generateImage" {
		t.Errorf("tool name = %v", tool0["name"])
	}
	schema, _ := tool0["inputSchema"].(map[string]interface{})
	if schema == nil {
		t.Fatal("missing inputSchema")
	}
	props, _ := schema["properties"].(map[string]interface{})
	if props["prompt"] == nil {
		t.Error("missing prompt property")
	}
	required, _ := schema["required"].([]interface{})
	if len(required) != 1 || required[0] != "prompt" {
		t.Errorf("required = %v, want [prompt]", required)
	}

	tool1 := tools[1].(map[string]interface{})
	if tool1["name"] != "testVid_generateVideo" {
		t.Errorf("tool name = %v", tool1["name"])
	}
}

func TestToolsCall(t *testing.T) {
	imgSup := &mockImageSupplier{
		name: "mockImg",
		genImage: func(req supplier.ImageRequest) *supplier.ImageResult {
			if req.Prompt == "" {
				t.Error("GenImage called with empty prompt")
			}
			return &supplier.ImageResult{
				URLs:      []string{"https://example.com/out.png"},
				ModelUsed: "mock-model-v2",
				Request:   req,
			}
		},
	}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 3, map[string]interface{}{
		"name": "mockImg_generateImage",
		"arguments": map[string]interface{}{
			"prompt": "a cat",
			"model":  "mock-model-v2",
			"n":      1,
		},
	})
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("tools/call error: %v", resp["error"])
	}
	result, _ := resp["result"].(map[string]interface{})
	if isErr, _ := result["isError"].(bool); isErr {
		t.Error("isError should be false")
	}
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatal("content is empty")
	}
	textItem := content[0].(map[string]interface{})
	if textItem["type"] != "text" {
		t.Errorf("content[0].type = %v", textItem["type"])
	}
	text := textItem["text"].(string)
	if !strings.Contains(text, "https://example.com/out.png") {
		t.Errorf("text should contain URL, got: %s", text)
	}
	if len(content) < 2 {
		t.Fatal("expected resource_link content block for image URL")
	}
	linkItem := content[1].(map[string]interface{})
	if linkItem["type"] != "resource_link" {
		t.Errorf("content[1].type = %v, want resource_link", linkItem["type"])
	}
	if linkItem["uri"] != "https://example.com/out.png" {
		t.Errorf("content[1].uri = %v", linkItem["uri"])
	}
	if linkItem["name"] != "out.png" {
		t.Errorf("content[1].name = %v, want out.png", linkItem["name"])
	}
}

func TestToolsCall_unknownTool(t *testing.T) {
	imgSup := &mockImageSupplier{name: "mock"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 4, map[string]interface{}{
		"name": "nonexistent_generateImage",
		"arguments": map[string]interface{}{
			"prompt": "test",
		},
	})
	resp := h.readResponse()
	if resp["error"] == nil {
		t.Fatal("expected error for unknown tool")
	}
	errObj := resp["error"].(map[string]interface{})
	code, _ := errObj["code"].(float64)
	if code != -32603 {
		t.Errorf("error code = %v, want -32603", code)
	}
}

func TestToolsCall_invalidParams(t *testing.T) {
	imgSup := &mockImageSupplier{name: "mock"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 5, "not-an-object")
	resp := h.readResponse()
	if resp["error"] == nil {
		t.Fatal("expected error for invalid params")
	}
	errObj := resp["error"].(map[string]interface{})
	code, _ := errObj["code"].(float64)
	if code != -32602 {
		t.Errorf("error code = %v, want -32602", code)
	}
}

func TestToolsCall_video(t *testing.T) {
	vidSup := &mockVideoSupplier{
		name: "mockVid",
		genVideo: func(req supplier.VideoRequest) *supplier.VideoResult {
			return &supplier.VideoResult{
				URLs:      []string{"https://example.com/video.mp4"},
				ModelUsed: "mock-video-model",
				Duration:  10,
				FrameRate: 24.0,
				Request:   req,
			}
		},
	}
	h := newMCPTestHarness(t, nil, []supplier.VideoSupplier{vidSup})
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 6, map[string]interface{}{
		"name": "mockVid_generateVideo",
		"arguments": map[string]interface{}{
			"prompt":   "a running dog",
			"duration": 5,
			"style":    "realistic",
		},
	})
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("tools/call error: %v", resp["error"])
	}
	result, _ := resp["result"].(map[string]interface{})
	if isErr, _ := result["isError"].(bool); isErr {
		t.Error("isError should be false")
	}
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatal("content is empty")
	}
	textItem := content[0].(map[string]interface{})
	text := textItem["text"].(string)
	if !strings.Contains(text, "https://example.com/video.mp4") {
		t.Errorf("text should contain video URL, got: %s", text)
	}
	if !strings.Contains(text, "Duration: 10 seconds") {
		t.Errorf("text should contain duration, got: %s", text)
	}
	if len(content) < 2 {
		t.Fatal("expected resource_link content block for video URL")
	}
	linkItem := content[1].(map[string]interface{})
	if linkItem["type"] != "resource_link" {
		t.Errorf("content[1].type = %v, want resource_link", linkItem["type"])
	}
	if linkItem["uri"] != "https://example.com/video.mp4" {
		t.Errorf("content[1].uri = %v", linkItem["uri"])
	}
	if linkItem["name"] != "video.mp4" {
		t.Errorf("content[1].name = %v, want video.mp4", linkItem["name"])
	}
}

func TestToolsList_videoStatusProvider(t *testing.T) {
	vidSup := &mockVideoStatusSupplier{mockVideoSupplier: mockVideoSupplier{name: "asyncVid"}}
	h := newMCPTestHarness(t, nil, []supplier.VideoSupplier{vidSup})
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/list", 2, nil)
	resp := h.readResponse()
	result, _ := resp["result"].(map[string]interface{})
	tools, _ := result["tools"].([]interface{})
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools (generateVideo + getVideoResult), got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tc := range tools {
		names[tc.(map[string]interface{})["name"].(string)] = true
	}
	if !names["asyncVid_generateVideo"] || !names["asyncVid_getVideoResult"] {
		t.Errorf("tool names = %v", names)
	}
}

func TestToolsCall_unknownArgs(t *testing.T) {
	imgSup := &mockImageSupplier{
		name: "mock",
		genImage: func(req supplier.ImageRequest) *supplier.ImageResult {
			return &supplier.ImageResult{URLs: []string{"https://example.com/img.png"}, ModelUsed: "mock", Request: req}
		},
	}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 15, map[string]interface{}{
		"name": "mock_generateImage",
		"arguments": map[string]interface{}{
			"prompt": "a cat",
			"ratio":  "16:9", // not declared by this supplier
		},
	})
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("tools/call error: %v", resp["error"])
	}
	result, _ := resp["result"].(map[string]interface{})
	content, _ := result["content"].([]interface{})
	textItem := content[0].(map[string]interface{})
	text := textItem["text"].(string)
	if !strings.Contains(text, "unexpected argument(s) ignored: ratio") {
		t.Errorf("text should flag the dropped argument, got: %s", text)
	}
}

func TestToolsList_getVideoResultSchema(t *testing.T) {
	vidSup := &mockVideoStatusSupplier{mockVideoSupplier: mockVideoSupplier{name: "asyncVid"}}
	h := newMCPTestHarness(t, nil, []supplier.VideoSupplier{vidSup})
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/list", 2, nil)
	resp := h.readResponse()
	result, _ := resp["result"].(map[string]interface{})
	tools, _ := result["tools"].([]interface{})
	var schema map[string]interface{}
	for _, tc := range tools {
		tool := tc.(map[string]interface{})
		if tool["name"] == "asyncVid_getVideoResult" {
			schema, _ = tool["inputSchema"].(map[string]interface{})
		}
	}
	if schema == nil {
		t.Fatal("missing getVideoResult inputSchema")
	}
	anyOf, _ := schema["anyOf"].([]interface{})
	if len(anyOf) != 2 {
		t.Fatalf("anyOf = %v, want 2 alternatives (task_id or video_id)", anyOf)
	}
}

func TestToolsCall_getVideoResult(t *testing.T) {
	vidSup := &mockVideoStatusSupplier{
		mockVideoSupplier: mockVideoSupplier{name: "asyncVid"},
		getResult: func(taskID, videoID string) *supplier.VideoResult {
			return &supplier.VideoResult{Status: "working", TaskID: taskID, ModelUsed: "mock", Request: supplier.VideoRequest{Supplier: "asyncVid"}}
		},
	}
	h := newMCPTestHarness(t, nil, []supplier.VideoSupplier{vidSup})
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 6, map[string]interface{}{
		"name": "asyncVid_getVideoResult",
		"arguments": map[string]interface{}{
			"task_id": "task_abc",
		},
	})
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("tools/call error: %v", resp["error"])
	}
	result, _ := resp["result"].(map[string]interface{})
	if isErr, _ := result["isError"].(bool); isErr {
		t.Error("isError should be false for working status")
	}
	content, _ := result["content"].([]interface{})
	textItem := content[0].(map[string]interface{})
	text := textItem["text"].(string)
	if !strings.Contains(text, "in progress") || !strings.Contains(text, "task_abc") || !strings.Contains(text, "asyncVid_getVideoResult") {
		t.Errorf("text should mention in-progress + task_id + poll tool, got: %s", text)
	}
}

func TestPing(t *testing.T) {
	imgSup := &mockImageSupplier{name: "mock"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("ping", 1, nil)
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("ping error: %v", resp["error"])
	}
	if resp["result"] == nil {
		t.Error("ping result should not be nil")
	}
}

func TestMethodNotFound(t *testing.T) {
	imgSup := &mockImageSupplier{name: "mock"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("unknown_method", 1, nil)
	resp := h.readResponse()
	if resp["error"] == nil {
		t.Fatal("expected error for unknown method")
	}
	errObj := resp["error"].(map[string]interface{})
	code, _ := errObj["code"].(float64)
	if code != -32601 {
		t.Errorf("error code = %v, want -32601", code)
	}
}

func TestParseError(t *testing.T) {
	imgSup := &mockImageSupplier{name: "mock"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	_, err := h.stdinPipe.Write([]byte("{invalid json}\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	resp := h.readResponse()
	if resp["error"] == nil {
		t.Fatal("expected parse error")
	}
	errObj := resp["error"].(map[string]interface{})
	code, _ := errObj["code"].(float64)
	if code != -32700 {
		t.Errorf("error code = %v, want -32700", code)
	}
}

func TestNotificationsAreIgnored(t *testing.T) {
	imgSup := &mockImageSupplier{name: "mock"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendNotification("notifications/cancelled", map[string]interface{}{
		"requestId": 1,
	})
	time.Sleep(100 * time.Millisecond)
	h.sendRequest("ping", 2, nil)
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("ping after notification error: %v", resp["error"])
	}
	if resp["result"] == nil {
		t.Error("ping result should not be nil")
	}
}



func TestToolsCall_errorResult(t *testing.T) {
	imgSup := &mockImageSupplier{
		name: "mockErr",
		genImage: func(req supplier.ImageRequest) *supplier.ImageResult {
			return &supplier.ImageResult{
				Error:   errors.New("API rate limit exceeded"),
				URLs:    []string{},
				Request: req,
			}
		},
	}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 7, map[string]interface{}{
		"name": "mockErr_generateImage",
		"arguments": map[string]interface{}{
			"prompt": "test",
		},
	})
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("unexpected protocol error: %v", resp["error"])
	}
	result, _ := resp["result"].(map[string]interface{})
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Error("isError should be true for failed tool call")
	}
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatal("content is empty")
	}
	textItem := content[0].(map[string]interface{})
	text := textItem["text"].(string)
	if !strings.Contains(text, "Error") || !strings.Contains(text, "API rate limit exceeded") {
		t.Errorf("text should contain error info, got: %s", text)
	}
}

func TestJSONRPCVersion(t *testing.T) {
	imgSup := &mockImageSupplier{name: "mock"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	for _, id := range []int{1, 2, 3} {
		h.sendRequest("ping", id, nil)
		resp := h.readResponse()
		if resp["jsonrpc"] != "2.0" {
			t.Errorf("request %d: jsonrpc = %v", id, resp["jsonrpc"])
		}
		if resp["id"] != float64(id) {
			t.Errorf("request %d: id = %v, want %d", id, resp["id"], id)
		}
	}
}

func TestEmptySuppliers(t *testing.T) {
	h := newMCPTestHarness(t, nil, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/list", 2, nil)
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("tools/list error: %v", resp["error"])
	}
	result, _ := resp["result"].(map[string]interface{})
	tools, _ := result["tools"].([]interface{})
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestToolsCall_multipleImageSuppliers(t *testing.T) {
	sup1 := &mockImageSupplier{
		name: "alpha",
		genImage: func(req supplier.ImageRequest) *supplier.ImageResult {
			return &supplier.ImageResult{URLs: []string{"https://alpha.com/img.png"}, ModelUsed: "alpha", Request: req}
		},
	}
	sup2 := &mockImageSupplier{
		name: "beta",
		genImage: func(req supplier.ImageRequest) *supplier.ImageResult {
			return &supplier.ImageResult{URLs: []string{"https://beta.com/img.png"}, ModelUsed: "beta", Request: req}
		},
	}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{sup1, sup2}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 10, map[string]interface{}{
		"name": "alpha_generateImage",
		"arguments": map[string]interface{}{
			"prompt": "from alpha",
		},
	})
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("alpha call error: %v", resp["error"])
	}
	result, _ := resp["result"].(map[string]interface{})
	content, _ := result["content"].([]interface{})
	textItem := content[0].(map[string]interface{})
	text := textItem["text"].(string)
	if !strings.Contains(text, "https://alpha.com/img.png") {
		t.Errorf("should call alpha, got: %s", text)
	}

	h.sendRequest("tools/call", 11, map[string]interface{}{
		"name": "beta_generateImage",
		"arguments": map[string]interface{}{
			"prompt": "from beta",
		},
	})
	resp = h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("beta call error: %v", resp["error"])
	}
	result, _ = resp["result"].(map[string]interface{})
	content, _ = result["content"].([]interface{})
	textItem = content[0].(map[string]interface{})
	text = textItem["text"].(string)
	if !strings.Contains(text, "https://beta.com/img.png") {
		t.Errorf("should call beta, got: %s", text)
	}
}

func TestToolsCall_extraArguments(t *testing.T) {
	var capturedReq supplier.ImageRequest
	imgSup := &mockImageSupplier{
		name: "mock",
		genImage: func(req supplier.ImageRequest) *supplier.ImageResult {
			capturedReq = req
			return &supplier.ImageResult{URLs: []string{"https://example.com/img.png"}, ModelUsed: "mock", Request: req}
		},
	}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 12, map[string]interface{}{
		"name": "mock_generateImage",
		"arguments": map[string]interface{}{
			"prompt": "test prompt",
			"model":  "test-model",
			"size":   "1024x768",
			"n":      3,
		},
	})
	h.readResponse()

	if capturedReq.Prompt != "test prompt" {
		t.Errorf("Prompt = %q", capturedReq.Prompt)
	}
	if capturedReq.Model != "test-model" {
		t.Errorf("Model = %q", capturedReq.Model)
	}
	if capturedReq.Size != "1024x768" {
		t.Errorf("Size = %q", capturedReq.Size)
	}
	if capturedReq.N != 3 {
		t.Errorf("N = %d", capturedReq.N)
	}
	if capturedReq.Extra != nil {
		t.Errorf("Extra = %v, want nil (supplier declares no extra params)", capturedReq.Extra)
	}
}

func TestToolsList_schemaExtender(t *testing.T) {
	imgSup := &mockExtImageSupplier{
		mockImageSupplier: mockImageSupplier{name: "extImg"},
		extraSchema: map[string]interface{}{
			"ratio": map[string]interface{}{"type": "string"},
			"image": map[string]interface{}{"type": "string"},
		},
	}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/list", 2, nil)
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("tools/list error: %v", resp["error"])
	}
	result, _ := resp["result"].(map[string]interface{})
	tools, _ := result["tools"].([]interface{})
	schema, _ := tools[0].(map[string]interface{})["inputSchema"].(map[string]interface{})
	props, _ := schema["properties"].(map[string]interface{})
	for _, k := range []string{"prompt", "ratio", "image"} {
		if props[k] == nil {
			t.Errorf("missing schema property %q", k)
		}
	}
}

func TestToolsCall_schemaExtenderForward(t *testing.T) {
	var capturedReq supplier.ImageRequest
	imgSup := &mockExtImageSupplier{
		mockImageSupplier: mockImageSupplier{
			name: "extImg",
			genImage: func(req supplier.ImageRequest) *supplier.ImageResult {
				capturedReq = req
				return &supplier.ImageResult{URLs: []string{"https://example.com/img.png"}, ModelUsed: "mock", Request: req}
			},
		},
		extraSchema: map[string]interface{}{
			"ratio": map[string]interface{}{"type": "string"},
			"image": map[string]interface{}{"type": "string"},
		},
	}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 14, map[string]interface{}{
		"name": "extImg_generateImage",
		"arguments": map[string]interface{}{
			"prompt": "a cat",
			"ratio":  "16:9",
			"image":  "https://example.com/ref.png",
		},
	})
	h.readResponse()

	if capturedReq.Prompt != "a cat" {
		t.Errorf("Prompt = %q", capturedReq.Prompt)
	}
	if capturedReq.Extra == nil || capturedReq.Extra["ratio"] != "16:9" {
		t.Errorf("ratio not forwarded to Extra: %v", capturedReq.Extra)
	}
	if capturedReq.Extra == nil || capturedReq.Extra["image"] != "https://example.com/ref.png" {
		t.Errorf("image not forwarded to Extra: %v", capturedReq.Extra)
	}
}

func TestToolsCall_videoExtraArguments(t *testing.T) {
	var capturedReq supplier.VideoRequest
	vidSup := &mockVideoSupplier{
		name: "mockVid",
		genVideo: func(req supplier.VideoRequest) *supplier.VideoResult {
			capturedReq = req
			return &supplier.VideoResult{URLs: []string{"https://example.com/vid.mp4"}, ModelUsed: "mock", Request: req}
		},
	}
	h := newMCPTestHarness(t, nil, []supplier.VideoSupplier{vidSup})
	defer h.close()

	h.sendRequest("initialize", 1, nil)
	h.readResponse()
	h.readResponse()

	h.sendRequest("tools/call", 13, map[string]interface{}{
		"name": "mockVid_generateVideo",
		"arguments": map[string]interface{}{
			"prompt":   "test video",
			"model":    "vid-model",
			"duration": 15,
			"style":    "cinematic",
			"seed":     42,
		},
	})
	h.readResponse()

	if capturedReq.Prompt != "test video" {
		t.Errorf("Prompt = %q", capturedReq.Prompt)
	}
	if capturedReq.Model != "vid-model" {
		t.Errorf("Model = %q", capturedReq.Model)
	}
	if capturedReq.Duration != 15 {
		t.Errorf("Duration = %d", capturedReq.Duration)
	}
	if capturedReq.Style != "cinematic" {
		t.Errorf("Style = %q", capturedReq.Style)
	}
	if capturedReq.Seed == nil || *capturedReq.Seed != 42 {
		t.Errorf("Seed = %v, want 42", capturedReq.Seed)
	}
}

// ---------------------------------------------------------------------------
// Unit tests for helper functions
// ---------------------------------------------------------------------------



func TestTrimTrailing(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello\n", "hello"},
		{"hello\r\n", "hello"},
		{"hello\r", "hello"},
		{"hello", "hello"},
		{"", ""},
		{"\n", ""},
		{"multi\nline\n", "multi\nline"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := trimTrailing(tt.input)
			if got != tt.want {
				t.Errorf("trimTrailing(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetString(t *testing.T) {
	m := map[string]interface{}{
		"name": "alice",
		"age":  30,
		"nil":  nil,
	}
	if got := getString(m, "name"); got != "alice" {
		t.Errorf("getString(name) = %q", got)
	}
	if got := getString(m, "age"); got != "" {
		t.Errorf("getString(age) = %q", got)
	}
	if got := getString(m, "nil"); got != "" {
		t.Errorf("getString(nil) = %q", got)
	}
	if got := getString(m, "missing"); got != "" {
		t.Errorf("getString(missing) = %q", got)
	}
}

func TestGetInt(t *testing.T) {
	m := map[string]interface{}{
		"count":  float64(42),
		"zero":   float64(0),
		"neg":    float64(-5),
		"string": "hello",
	}
	if got := getInt(m, "count"); got != 42 {
		t.Errorf("getInt(count) = %d", got)
	}
	if got := getInt(m, "zero"); got != 0 {
		t.Errorf("getInt(zero) = %d", got)
	}
	if got := getInt(m, "neg"); got != -5 {
		t.Errorf("getInt(neg) = %d", got)
	}
	if got := getInt(m, "string"); got != 0 {
		t.Errorf("getInt(string) = %d", got)
	}
	if got := getInt(m, "missing"); got != 0 {
		t.Errorf("getInt(missing) = %d", got)
	}
}

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		ss   []string
		sep  string
		want string
	}{
		{[]string{"a", "b", "c"}, ", ", "a, b, c"},
		{[]string{"a"}, ", ", "a"},
		{[]string{}, ", ", ""},
		{nil, ", ", ""},
		{[]string{"a", "b"}, "", "ab"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.ss), func(t *testing.T) {
			got := joinStrings(tt.ss, tt.sep)
			if got != tt.want {
				t.Errorf("joinStrings(%v, %q) = %q, want %q", tt.ss, tt.sep, got, tt.want)
			}
		})
	}
}

func TestOnlyInitializeSendsNotification(t *testing.T) {
	imgSup := &mockImageSupplier{name: "mock"}
	h := newMCPTestHarness(t, []supplier.ImageSupplier{imgSup}, nil)
	defer h.close()

	h.sendRequest("ping", 1, nil)
	resp := h.readResponse()
	if resp["error"] != nil {
		t.Fatalf("ping error: %v", resp["error"])
	}
	// Ping should not trigger a notification; reading another message must time out.
	ch := make(chan struct{}, 1)
	go func() {
		var buf [1]byte
		h.stdoutPipe.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _ := h.stdoutPipe.Read(buf[:])
		if n > 0 {
			ch <- struct{}{}
		}
	}()
	select {
	case <-ch:
		t.Error("unexpected second message after ping")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestJsonNumberMarshal(t *testing.T) {
	j := &jsonNumber{Num: 42, Seen: true}
	data, err := j.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(data) != "42" {
		t.Errorf("MarshalJSON = %s, want 42", string(data))
	}

	j2 := &jsonNumber{Seen: false}
	data2, err := j2.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(data2) != "null" {
		t.Errorf("MarshalJSON = %s, want null", string(data2))
	}
}

func TestJsonNumberUnmarshal(t *testing.T) {
	j := &jsonNumber{}
	if err := j.UnmarshalJSON([]byte("42")); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if j.Num != 42 || !j.Seen {
		t.Errorf("Num = %d, Seen = %v, want 42, true", j.Num, j.Seen)
	}
}

func TestJsonNumberUnmarshalError(t *testing.T) {
	j := &jsonNumber{}
	err := j.UnmarshalJSON([]byte("not-a-number"))
	if err == nil {
		t.Error("expected error unmarshaling non-number")
	}
}



func TestMimeTypeFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		// Standard extensions
		{"https://example.com/image.png", "image/png"},
		{"https://example.com/photo.jpg", "image/jpeg"},
		{"https://example.com/photo.jpeg", "image/jpeg"},
		{"https://example.com/anim.webp", "image/webp"},
		{"https://example.com/anim.gif", "image/gif"},
		{"https://example.com/video.mp4", "video/mp4"},
		{"https://example.com/video.webm", "video/webm"},
		{"https://example.com/video.avi", "video/x-msvideo"},
		{"https://example.com/video.mov", "video/quicktime"},
		// Uppercase extensions
		{"https://example.com/image.PNG", "image/png"},
		{"https://example.com/photo.JPG", "image/jpeg"},
		{"https://example.com/video.MP4", "video/mp4"},
		{"https://example.com/video.MOV", "video/quicktime"},
		// Query strings
		{"https://cdn.example.com/image.png?x=1", "image/png"},
		{"https://cdn.example.com/video.mp4?token=abc&expires=999", "video/mp4"},
		{"https://cdn.example.com/photo.jpg?w=800&h=600", "image/jpeg"},
		{"https://cdn.example.com/anim.gif?x=1&y=2#frag", "image/gif"},
		// No extension / unknown
		{"https://example.com/photo", "application/octet-stream"},
		{"https://example.com/dir.with.dots/file", "application/octet-stream"},
		{"https://example.com/image.png?x=1/unknown", "image/png"},
		// Local file paths
		{"file:///tmp/image.png", "image/png"},
		{"/tmp/photo.jpg", "image/jpeg"},
		// Base64-like (no extension at end)
		{"data:image/png;base64,abc", "application/octet-stream"},
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			got := mimeTypeFromURL(tc.url)
			if got != tc.want {
				t.Errorf("mimeTypeFromURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}
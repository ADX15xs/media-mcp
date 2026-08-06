package supplier

import "time"

// imageHTTPTimeout caps a single synchronous image-generation HTTP call so it
// fails with a clean, retriable error before the MCP client's request timeout
// fires an opaque -32001. Image APIs are synchronous, so this is the best a
// server-side adapter can do.
const imageHTTPTimeout = 50 * time.Second

// statusPollCap bounds a single getVideoResult call: it polls until the task
// reaches a terminal state or the cap elapses, then returns "working" so the
// caller retries. Each call stays well under any client request timeout.
const statusPollCap = 20 * time.Second

// ImageRequest holds a unified image generation request.
type ImageRequest struct {
	Supplier    string                 // e.g. "senseNova"
	Prompt      string                 // required
	Model       string                 // supplier-specific model ID or name
	Size        string                 // "2752x1536", etc.
	N           int                    // number of images (default 1)
	Extra       map[string]interface{} // supplier-specific params (declared via SchemaExtender)
	UnknownArgs []string               // unrecognized arguments, dropped but echoed to the caller
}

// ImageResult is the unified result from an image generation call.
type ImageResult struct {
	URLs     []string           // public URLs to generated images (may be empty)
	Base64   []string           // base64-encoded image data (alternative to URLs)
	ModelUsed string            // which model was actually used
	Request  ImageRequest      // echo of the request for tracing
	Error    error             // non-nil if the call failed
}

// VideoRequest holds a unified video generation request.
type VideoRequest struct {
	Supplier    string                 // e.g. "runway"
	Prompt      string                 // required
	Model       string                 // supplier-specific model ID or name
	Duration    int                    // duration in seconds
	Style       string                 // style hint
	Seed        *int                   // reproducibility seed
	AspectRatio string                 // optional, e.g. "9:16"; provider maps to width/height or ignores
	Resolution  string                 // optional, e.g. "720p"; provider maps to width/height or ignores
	Extra       map[string]interface{} // supplier-specific params (declared via SchemaExtender)
	UnknownArgs []string               // unrecognized arguments, dropped but echoed to the caller
}

// VideoResult is the unified result from a video generation call.
type VideoResult struct {
	URLs      []string
	ModelUsed string
	FrameRate float64
	Duration  int
	Request   VideoRequest
	Error     error

	// Status is "working", "completed", or "failed". Empty is treated as
	// completed for backward compatibility with synchronous-style results.
	Status string
	// TaskID/VideoID identify an in-progress task returned by a non-blocking
	// submit; the caller polls getVideoResult with them.
	TaskID  string
	VideoID string
}

// ImageSupplier is implemented by each vendor adapter.
type ImageSupplier interface {
	Name() string                        // unique supplier name
	GenImage(req ImageRequest) *ImageResult // call the supplier's image API
}

// VideoSupplier is implemented by each vendor adapter.
type VideoSupplier interface {
	Name() string                         // unique supplier name
	GenVideo(req VideoRequest) *VideoResult // call the supplier's video API
}

// CapabilityProvider is an optional interface a supplier can implement to
// expose capability notes that the MCP server appends to the tool description,
// so agents can pick valid parameters without external docs.
type CapabilityProvider interface {
	Capabilities() string
}

// SchemaExtender lets a supplier declare provider-specific tool parameters:
// the transport merges ExtraInputSchema into the tool's inputSchema and
// forwards matching call arguments to the supplier via req.Extra.
type SchemaExtender interface {
	ExtraInputSchema() map[string]interface{}
}

// VideoStatusProvider is an optional interface for video suppliers that submit
// asynchronously. GenVideo returns a working result with a TaskID; the caller
// polls GetVideoResult until the task reaches a terminal state.
type VideoStatusProvider interface {
	GetVideoResult(taskID, videoID string) *VideoResult
}

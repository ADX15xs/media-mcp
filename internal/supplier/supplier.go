package supplier

// ImageRequest holds a unified image generation request.
type ImageRequest struct {
	Supplier   string                 // e.g. "senseNova"
	Prompt     string                 // required
	NegativePrompt string             // optional, some suppliers support it
	Model      string                 // supplier-specific model ID or name
	Size       string                 // "2752x1536", etc.
	N          int                    // number of images (default 1)
	Style      string                 // style hint for the supplier
	Extra      map[string]interface{} // arbitrary extra fields passed to the supplier
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
	Supplier   string                 // e.g. "runway"
	Prompt     string                 // required
	Model      string                 // supplier-specific model ID or name
	Duration   int                    // duration in seconds
	Style      string                 // style hint
	Seed       *int                   // reproducibility seed
	Extra      map[string]interface{} // arbitrary extra fields passed to the supplier
}

// VideoResult is the unified result from a video generation call.
type VideoResult struct {
	URLs       []string          // video URLs (usually one)
	ModelUsed  string            // which model was used
	FrameRate  float64           // fps of output
	Duration   int               // actual duration in seconds
	Request    VideoRequest      // echo of the request
	Error      error             // non-nil if the call failed
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

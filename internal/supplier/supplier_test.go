package supplier

import (
	"errors"
	"media-mcp/internal/config"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}
}

func TestRegistryRegisterAndBuildImage(t *testing.T) {
	r := NewRegistry()
	r.RegisterImage("test_img", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return &mockImageSup{name: "test_img"}, nil
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"test_img": {Enabled: true, SupplierType: "image", APIKey: "key", BaseURL: "https://example.com"},
		},
	}

	suppliers, err := r.BuildImage(cfg)
	if err != nil {
		t.Fatalf("BuildImage() error = %v", err)
	}
	if len(suppliers) != 1 {
		t.Fatalf("expected 1 supplier, got %d", len(suppliers))
	}
	if suppliers[0].Name() != "test_img" {
		t.Errorf("Name() = %q", suppliers[0].Name())
	}
}

func TestRegistryBuildImage_skipsVideo(t *testing.T) {
	r := NewRegistry()
	r.RegisterImage("img_sup", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return &mockImageSup{name: "img_sup"}, nil
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"img_sup": {Enabled: true, SupplierType: "video", APIKey: "key", BaseURL: "https://example.com"},
		},
	}

	suppliers, err := r.BuildImage(cfg)
	if err != nil {
		t.Fatalf("BuildImage() error = %v", err)
	}
	if len(suppliers) != 0 {
		t.Errorf("expected 0 image suppliers for video type, got %d", len(suppliers))
	}
}

func TestRegistryBuildImage_skipsDisabled(t *testing.T) {
	r := NewRegistry()
	r.RegisterImage("disabled_sup", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return &mockImageSup{name: "disabled_sup"}, nil
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"disabled_sup": {Enabled: false, SupplierType: "image", APIKey: "key", BaseURL: "https://example.com"},
		},
	}

	suppliers, err := r.BuildImage(cfg)
	if err != nil {
		t.Fatalf("BuildImage() error = %v", err)
	}
	if len(suppliers) != 0 {
		t.Errorf("expected 0 suppliers for disabled, got %d", len(suppliers))
	}
}

func TestRegistryBuildImage_unregistered(t *testing.T) {
	r := NewRegistry()
	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"unknown": {Enabled: true, SupplierType: "image", APIKey: "key", BaseURL: "https://example.com"},
		},
	}

	_, err := r.BuildImage(cfg)
	if err == nil {
		t.Fatal("expected error for unregistered supplier")
	}
}

func TestRegistryBuildImage_builderError(t *testing.T) {
	r := NewRegistry()
	r.RegisterImage("broken", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return nil, errors.New("builder failed")
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"broken": {Enabled: true, SupplierType: "image", APIKey: "key", BaseURL: "https://example.com"},
		},
	}

	_, err := r.BuildImage(cfg)
	if err == nil {
		t.Fatal("expected error from builder")
	}
}

func TestRegistryRegisterAndBuildVideo(t *testing.T) {
	r := NewRegistry()
	r.RegisterVideo("test_vid", func(cfg *config.SupplierConfig) (VideoSupplier, error) {
		return &mockVideoSup{name: "test_vid"}, nil
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"test_vid": {Enabled: true, SupplierType: "video", APIKey: "key", BaseURL: "https://example.com"},
		},
	}

	suppliers, err := r.BuildVideo(cfg)
	if err != nil {
		t.Fatalf("BuildVideo() error = %v", err)
	}
	if len(suppliers) != 1 {
		t.Fatalf("expected 1 video supplier, got %d", len(suppliers))
	}
	if suppliers[0].Name() != "test_vid" {
		t.Errorf("Name() = %q", suppliers[0].Name())
	}
}

func TestRegistryBuildVideo_bothType(t *testing.T) {
	r := NewRegistry()
	r.RegisterVideo("both_sup", func(cfg *config.SupplierConfig) (VideoSupplier, error) {
		return &mockVideoSup{name: "both_sup"}, nil
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"both_sup": {Enabled: true, SupplierType: "both", APIKey: "key", BaseURL: "https://example.com"},
		},
	}

	suppliers, err := r.BuildVideo(cfg)
	if err != nil {
		t.Fatalf("BuildVideo() error = %v", err)
	}
	if len(suppliers) != 1 {
		t.Errorf("expected 1 video supplier for 'both' type, got %d", len(suppliers))
	}
}

func TestRegistryBuildVideo_skipsImage(t *testing.T) {
	r := NewRegistry()
	r.RegisterVideo("img_only", func(cfg *config.SupplierConfig) (VideoSupplier, error) {
		return &mockVideoSup{name: "img_only"}, nil
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"img_only": {Enabled: true, SupplierType: "image", APIKey: "key", BaseURL: "https://example.com"},
		},
	}

	suppliers, err := r.BuildVideo(cfg)
	if err != nil {
		t.Fatalf("BuildVideo() error = %v", err)
	}
	if len(suppliers) != 0 {
		t.Errorf("expected 0 video suppliers for image type, got %d", len(suppliers))
	}
}

// ---------------------------------------------------------------------------
// Mock suppliers for registry tests
// ---------------------------------------------------------------------------

type mockImageSup struct {
	name string
}

func (m *mockImageSup) Name() string { return m.name }

func (m *mockImageSup) GenImage(req ImageRequest) *ImageResult {
	return &ImageResult{URLs: []string{"https://mock.example.com/img.png"}, ModelUsed: "mock", Request: req}
}

type mockVideoSup struct {
	name string
}

func (m *mockVideoSup) Name() string { return m.name }

func (m *mockVideoSup) GenVideo(req VideoRequest) *VideoResult {
	return &VideoResult{URLs: []string{"https://mock.example.com/vid.mp4"}, ModelUsed: "mock", Request: req}
}

// ---------------------------------------------------------------------------
// ImageRequest/VideoRequest constructor validation
// ---------------------------------------------------------------------------

func TestImageRequestDefaults(t *testing.T) {
	req := ImageRequest{
		Supplier: "test",
		Prompt:   "hello",
	}
	if req.Supplier != "test" {
		t.Errorf("Supplier = %q", req.Supplier)
	}
	if req.Prompt != "hello" {
		t.Errorf("Prompt = %q", req.Prompt)
	}
	if req.N != 0 {
		t.Errorf("N = %d, want 0", req.N)
	}
}

func TestVideoRequestDefaults(t *testing.T) {
	req := VideoRequest{
		Supplier: "test",
		Prompt:   "hello",
	}
	if req.Supplier != "test" {
		t.Errorf("Supplier = %q", req.Supplier)
	}
	if req.Prompt != "hello" {
		t.Errorf("Prompt = %q", req.Prompt)
	}
	if req.Duration != 0 {
		t.Errorf("Duration = %d, want 0", req.Duration)
	}
}

func TestImageResult(t *testing.T) {
	r := &ImageResult{
		URLs:      []string{"https://example.com/img.png"},
		ModelUsed: "model-x",
		Error:     nil,
	}
	if len(r.URLs) != 1 {
		t.Errorf("URLs = %v", r.URLs)
	}
	if r.Error != nil {
		t.Errorf("Error = %v", r.Error)
	}
}

func TestVideoResult(t *testing.T) {
	r := &VideoResult{
		URLs:      []string{"https://example.com/vid.mp4"},
		ModelUsed: "model-y",
		FrameRate: 30.0,
		Duration:  15,
		Error:     nil,
	}
	if len(r.URLs) != 1 {
		t.Errorf("URLs = %v", r.URLs)
	}
	if r.FrameRate != 30.0 {
		t.Errorf("FrameRate = %v", r.FrameRate)
	}
}
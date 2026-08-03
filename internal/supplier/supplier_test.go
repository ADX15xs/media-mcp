package supplier

import (
	"errors"
	"fmt"
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

	suppliers, err := r.BuildImage(cfg)
	if err != nil {
		t.Fatalf("BuildImage() unexpected error = %v", err)
	}
	if len(suppliers) != 1 {
		t.Fatalf("expected 1 supplier (fallback generic), got %d", len(suppliers))
	}
	// The fallback should be an HTTPGenericAdapter.
	if _, ok := suppliers[0].(*HTTPGenericAdapter); !ok {
		t.Errorf("expected HTTPGenericAdapter, got %T", suppliers[0])
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
// BuildAll tests
// ---------------------------------------------------------------------------

func TestBuildAll_imageAndVideo(t *testing.T) {
	r := NewRegistry()
	r.RegisterImage("img_sup", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return &mockImageSup{name: "img_sup"}, nil
	})
	r.RegisterVideo("vid_sup", func(cfg *config.SupplierConfig) (VideoSupplier, error) {
		return &mockVideoSup{name: "vid_sup"}, nil
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"img_sup": {Enabled: true, SupplierType: "image", APIKey: "key", BaseURL: "https://img.example.com"},
			"vid_sup": {Enabled: true, SupplierType: "video", APIKey: "key", BaseURL: "https://vid.example.com"},
		},
	}

	images, videos, errs := r.BuildAll(cfg)
	if len(errs) > 0 {
		t.Fatalf("BuildAll() errors = %v", errs)
	}
	if len(images) != 1 {
		t.Errorf("expected 1 image supplier, got %d", len(images))
	}
	if len(videos) != 1 {
		t.Errorf("expected 1 video supplier, got %d", len(videos))
	}
	if images[0].Name() != "img_sup" {
		t.Errorf("image name = %q", images[0].Name())
	}
	if videos[0].Name() != "vid_sup" {
		t.Errorf("video name = %q", videos[0].Name())
	}
}

func TestBuildAll_bothType(t *testing.T) {
	r := NewRegistry()
	r.RegisterImage("both_sup", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return &mockImageSup{name: "both_sup"}, nil
	})
	r.RegisterVideo("both_sup", func(cfg *config.SupplierConfig) (VideoSupplier, error) {
		return &mockVideoSup{name: "both_sup"}, nil
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"both_sup": {Enabled: true, SupplierType: "both", APIKey: "key", BaseURL: "https://example.com"},
		},
	}

	images, videos, errs := r.BuildAll(cfg)
	if len(errs) > 0 {
		t.Fatalf("BuildAll() errors = %v", errs)
	}
	if len(images) != 1 {
		t.Errorf("expected 1 image supplier for 'both', got %d", len(images))
	}
	if len(videos) != 1 {
		t.Errorf("expected 1 video supplier for 'both', got %d", len(videos))
	}
}

func TestBuildAll_unregisteredFallback(t *testing.T) {
	r := NewRegistry()

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"unknown_img": {Enabled: true, SupplierType: "image", APIKey: "key", BaseURL: "https://img.example.com"},
			"unknown_vid": {Enabled: true, SupplierType: "video", APIKey: "key", BaseURL: "https://vid.example.com"},
		},
	}

	images, videos, errs := r.BuildAll(cfg)
	if len(errs) > 0 {
		t.Fatalf("BuildAll() unexpected errors = %v", errs)
	}
	if len(images) != 1 {
		t.Errorf("expected 1 image supplier (fallback), got %d", len(images))
	}
	if len(videos) != 1 {
		t.Errorf("expected 1 video supplier (fallback), got %d", len(videos))
	}
	if _, ok := images[0].(*HTTPGenericAdapter); !ok {
		t.Errorf("image fallback expected HTTPGenericAdapter, got %T", images[0])
	}
	if _, ok := videos[0].(*HTTPGenericVideoAdapter); !ok {
		t.Errorf("video fallback expected HTTPGenericVideoAdapter, got %T", videos[0])
	}
}

func TestBuildAll_skipsDisabled(t *testing.T) {
	r := NewRegistry()
	r.RegisterImage("enabled", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return &mockImageSup{name: "enabled"}, nil
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"enabled":  {Enabled: true, SupplierType: "image", APIKey: "key", BaseURL: "https://example.com"},
			"disabled": {Enabled: false, SupplierType: "image", APIKey: "key", BaseURL: "https://example.com"},
		},
	}

	images, videos, errs := r.BuildAll(cfg)
	if len(errs) > 0 {
		t.Fatalf("BuildAll() errors = %v", errs)
	}
	if len(images) != 1 {
		t.Errorf("expected 1 image supplier, got %d", len(images))
	}
	if len(videos) != 0 {
		t.Errorf("expected 0 video suppliers, got %d", len(videos))
	}
}

func TestBuildAll_deterministicOrder(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("sup_%d", i)
		r.RegisterImage(name, func(cfg *config.SupplierConfig) (ImageSupplier, error) {
			return &mockImageSup{name: name}, nil
		})
	}

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"sup_3": {Enabled: true, SupplierType: "image", APIKey: "k", BaseURL: "https://example.com"},
			"sup_1": {Enabled: true, SupplierType: "image", APIKey: "k", BaseURL: "https://example.com"},
			"sup_4": {Enabled: true, SupplierType: "image", APIKey: "k", BaseURL: "https://example.com"},
			"sup_0": {Enabled: true, SupplierType: "image", APIKey: "k", BaseURL: "https://example.com"},
			"sup_2": {Enabled: true, SupplierType: "image", APIKey: "k", BaseURL: "https://example.com"},
		},
	}

	// Run twice; order must be the same.
	order1 := ""
	images1, _, errs := r.BuildAll(cfg)
	if len(errs) > 0 {
		t.Fatalf("build 1: %v", errs)
	}
	for _, s := range images1 {
		order1 += s.Name() + ","
	}

	order2 := ""
	images2, _, errs := r.BuildAll(cfg)
	if len(errs) > 0 {
		t.Fatalf("build 2: %v", errs)
	}
	for _, s := range images2 {
		order2 += s.Name() + ","
	}

	expected := "sup_0,sup_1,sup_2,sup_3,sup_4,"
	if order1 != expected {
		t.Errorf("expected sorted order %q, got %q", expected, order1)
	}
	if order1 != order2 {
		t.Errorf("non-deterministic order: %q vs %q", order1, order2)
	}
}

func TestBuildAll_collectsErrors(t *testing.T) {
	r := NewRegistry()
	r.RegisterImage("good", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return &mockImageSup{name: "good"}, nil
	})
	r.RegisterImage("broken", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return nil, errors.New("builder failed")
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"good":   {Enabled: true, SupplierType: "image", APIKey: "k", BaseURL: "https://example.com"},
			"broken": {Enabled: true, SupplierType: "image", APIKey: "k", BaseURL: "https://example.com"},
		},
	}

	images, videos, errs := r.BuildAll(cfg)
	if len(images) != 1 {
		t.Errorf("expected 1 good supplier, got %d", len(images))
	}
	if len(videos) != 0 {
		t.Errorf("expected 0 video suppliers, got %d", len(videos))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if images[0].Name() != "good" {
		t.Errorf("expected good supplier, got %q", images[0].Name())
	}
}

// ---------------------------------------------------------------------------
// Global registry / BuildAll convenience wrapper
// ---------------------------------------------------------------------------

func TestGlobalRegisterAndBuildAll(t *testing.T) {
	// Register via the global registry, then use BuildAll().
	RegisterImage("test_global_img", func(cfg *config.SupplierConfig) (ImageSupplier, error) {
		return &mockImageSup{name: "test_global_img"}, nil
	})
	RegisterVideo("test_global_vid", func(cfg *config.SupplierConfig) (VideoSupplier, error) {
		return &mockVideoSup{name: "test_global_vid"}, nil
	})

	cfg := &config.GlobalConfig{
		Suppliers: map[string]config.SupplierConfig{
			"test_global_img": {Enabled: true, SupplierType: "image", APIKey: "k", BaseURL: "https://example.com"},
			"test_global_vid": {Enabled: true, SupplierType: "video", APIKey: "k", BaseURL: "https://example.com"},
		},
	}

	images, videos, errs := BuildAll(cfg)
	if len(errs) > 0 {
		t.Fatalf("BuildAll() errors = %v", errs)
	}
	if len(images) != 1 {
		t.Errorf("expected 1 image, got %d", len(images))
	}
	if len(videos) != 1 {
		t.Errorf("expected 1 video, got %d", len(videos))
	}
}

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

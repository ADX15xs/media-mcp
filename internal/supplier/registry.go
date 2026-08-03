package supplier

import (
	"fmt"
	"media-mcp/internal/config"
	"sort"
)

// Registry is the factory that maps supplier names to their adapters.
type Registry struct {
	imageImplementations map[string]configImageBuilder
	videoImplementations map[string]configVideoBuilder
}

type configImageBuilder func(cfg *config.SupplierConfig) (ImageSupplier, error)
type configVideoBuilder func(cfg *config.SupplierConfig) (VideoSupplier, error)

// ---------------------------------------------------------------------------
// Global default registry — adapters self-register via init().
// Registration happens only during init(); the registry is read-only at runtime,
// so no synchronization is needed.
// ---------------------------------------------------------------------------

var defaultRegistry = NewRegistry()

// RegisterImage adds a new image supplier implementation to the global registry.
func RegisterImage(name string, fn configImageBuilder) {
	defaultRegistry.RegisterImage(name, fn)
}

// RegisterVideo adds a new video supplier implementation to the global registry.
func RegisterVideo(name string, fn configVideoBuilder) {
	defaultRegistry.RegisterVideo(name, fn)
}

// BuildAll is a convenience wrapper that calls defaultRegistry.BuildAll(cfg).
func BuildAll(cfg *config.GlobalConfig) ([]ImageSupplier, []VideoSupplier, []error) {
	return defaultRegistry.BuildAll(cfg)
}

// ---------------------------------------------------------------------------
// Registry methods
// ---------------------------------------------------------------------------

func NewRegistry() *Registry {
	return &Registry{
		imageImplementations: make(map[string]configImageBuilder),
		videoImplementations: make(map[string]configVideoBuilder),
	}
}

// RegisterImage adds a new image supplier implementation by name.
func (r *Registry) RegisterImage(name string, fn configImageBuilder) {
	r.imageImplementations[name] = fn
}

// RegisterVideo adds a new video supplier implementation by name.
func (r *Registry) RegisterVideo(name string, fn configVideoBuilder) {
	r.videoImplementations[name] = fn
}

// BuildAll creates all enabled image and video suppliers from config.
// Unregistered suppliers fall back to HTTPGenericAdapter (image) or
// HTTPGenericVideoAdapter (video). Errors are collected but do not
// abort the entire build — failed suppliers are skipped.
func (r *Registry) BuildAll(cfg *config.GlobalConfig) ([]ImageSupplier, []VideoSupplier, []error) {
	// Sort keys for deterministic order.
	names := make([]string, 0, len(cfg.Suppliers))
	for n := range cfg.Suppliers {
		names = append(names, n)
	}
	sort.Strings(names)

	var imageSuppliers []ImageSupplier
	var videoSuppliers []VideoSupplier
	var errs []error

	for _, name := range names {
		sCfg := cfg.Suppliers[name]
		if !sCfg.Enabled {
			continue
		}

		// Build image supplier if type allows.
		if sCfg.SupplierType == "image" || sCfg.SupplierType == "both" {
			sup, err := r.buildImageOne(name, &sCfg)
			if err != nil {
				errs = append(errs, fmt.Errorf("image supplier %q: %w", name, err))
			} else {
				imageSuppliers = append(imageSuppliers, sup)
			}
		}

		// Build video supplier if type allows.
		if sCfg.SupplierType == "video" || sCfg.SupplierType == "both" {
			sup, err := r.buildVideoOne(name, &sCfg)
			if err != nil {
				errs = append(errs, fmt.Errorf("video supplier %q: %w", name, err))
			} else {
				videoSuppliers = append(videoSuppliers, sup)
			}
		}
	}

	return imageSuppliers, videoSuppliers, errs
}

// buildImageOne builds a single image supplier for the given name.
// Falls back to HTTPGenericAdapter if the name is not registered.
func (r *Registry) buildImageOne(name string, cfg *config.SupplierConfig) (ImageSupplier, error) {
	if fn, ok := r.imageImplementations[name]; ok {
		return fn(cfg)
	}
	// Fallback: generic OpenAI-compatible adapter.
	return NewHTTPGenericAdapter(name, cfg), nil
}

// buildVideoOne builds a single video supplier for the given name.
// Falls back to HTTPGenericVideoAdapter if the name is not registered.
func (r *Registry) buildVideoOne(name string, cfg *config.SupplierConfig) (VideoSupplier, error) {
	if fn, ok := r.videoImplementations[name]; ok {
		return fn(cfg)
	}
	// Fallback: generic video adapter.
	return NewHTTPGenericVideoAdapter(name, cfg), nil
}

// BuildImage creates all enabled image suppliers from config.
// Deprecated: use BuildAll instead.
func (r *Registry) BuildImage(cfg *config.GlobalConfig) ([]ImageSupplier, error) {
	suppliers, _, errs := r.BuildAll(cfg)
	if len(errs) > 0 {
		return suppliers, errs[0]
	}
	return suppliers, nil
}

// BuildVideo creates all enabled video suppliers from config.
// Deprecated: use BuildAll instead.
func (r *Registry) BuildVideo(cfg *config.GlobalConfig) ([]VideoSupplier, error) {
	_, suppliers, errs := r.BuildAll(cfg)
	if len(errs) > 0 {
		return suppliers, errs[0]
	}
	return suppliers, nil
}

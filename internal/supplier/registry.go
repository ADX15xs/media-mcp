package supplier

import (
	"fmt"
	"media-mcp/internal/config"
)

// Registry is the factory that maps supplier names to their adapters.
type Registry struct {
	imageImplementations map[string]configImageBuilder
	videoImplementations map[string]configVideoBuilder
}

type configImageBuilder func(cfg *config.SupplierConfig) (ImageSupplier, error)
type configVideoBuilder func(cfg *config.SupplierConfig) (VideoSupplier, error)

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

// BuildImage creates all enabled image suppliers from config.
func (r *Registry) BuildImage(cfg *config.GlobalConfig) ([]ImageSupplier, error) {
	var suppliers []ImageSupplier
	for name, sCfg := range cfg.Suppliers {
		if !sCfg.Enabled {
			continue
		}
		if sCfg.SupplierType == "video" {
			continue
		}
		fn, ok := r.imageImplementations[name]
		if !ok {
			return nil, fmt.Errorf("no image supplier registered for %q", name)
		}
		sup, err := fn(&sCfg)
		if err != nil {
			return nil, fmt.Errorf("build image supplier %q: %w", name, err)
		}
		suppliers = append(suppliers, sup)
	}
	return suppliers, nil
}

// BuildVideo creates all enabled video suppliers from config.
func (r *Registry) BuildVideo(cfg *config.GlobalConfig) ([]VideoSupplier, error) {
	var suppliers []VideoSupplier
	for name, sCfg := range cfg.Suppliers {
		if !sCfg.Enabled {
			continue
		}
		if sCfg.SupplierType != "video" && sCfg.SupplierType != "both" {
			continue
		}
		fn, ok := r.videoImplementations[name]
		if !ok {
			return nil, fmt.Errorf("no video supplier registered for %q", name)
		}
		sup, err := fn(&sCfg)
		if err != nil {
			return nil, fmt.Errorf("build video supplier %q: %w", name, err)
		}
		suppliers = append(suppliers, sup)
	}
	return suppliers, nil
}

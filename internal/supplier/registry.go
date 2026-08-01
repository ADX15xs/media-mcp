package supplier

import (
	"fmt"
	"media-mcp/internal/config"
)

// Registry is the factory that maps supplier names to their adapters.
type Registry struct {
	implementations map[string]configImageBuilder
}

type configImageBuilder func(cfg *config.SupplierConfig) (ImageSupplier, error)

func NewRegistry() *Registry {
	return &Registry{
		implementations: make(map[string]configImageBuilder),
	}
}

// RegisterImage adds a new image supplier implementation by name.
func (r *Registry) RegisterImage(name string, fn configImageBuilder) {
	r.implementations[name] = fn
}

// Build creates all enabled image suppliers from config.
func (r *Registry) Build(cfg *config.GlobalConfig) ([]ImageSupplier, error) {
	var suppliers []ImageSupplier
	for name, sCfg := range cfg.Suppliers {
		if !sCfg.Enabled {
			continue
		}
		if sCfg.SupplierType == "video" {
			continue // skip non-image suppliers
		}
		fn, ok := r.implementations[name]
		if !ok {
			return nil, fmt.Errorf("no image supplier registered for %q", name)
		}
		sup, err := fn(&sCfg)
		if err != nil {
			return nil, fmt.Errorf("build supplier %q: %w", name, err)
		}
		suppliers = append(suppliers, sup)
	}
	return suppliers, nil
}

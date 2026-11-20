package adapter

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type SchemaInfo struct {
	Schema       Schema       `json:"schema"`
	DisplayName  string       `json:"display_name"`
	Capabilities Capabilities `json:"capabilities"`
	Defaults     Defaults     `json:"defaults"`
	Domains      []string     `json:"domains,omitempty"`
}

type Registry struct {
	mu       sync.RWMutex
	bySchema map[Schema]Adapter
	byDomain map[string]Schema
	order    []Schema
}

func NewRegistry() *Registry {
	return &Registry{
		bySchema: map[Schema]Adapter{},
		byDomain: map[string]Schema{},
	}
}

func (r *Registry) MustRegister(a Adapter, domains ...string) {
	if a == nil {
		panic("adapter: nil adapter")
	}
	schema := a.Schema()
	if schema == "" {
		panic("adapter: empty schema")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, dup := r.bySchema[schema]; dup {
		panic(fmt.Sprintf("adapter: schema %q registered twice", schema))
	}
	r.bySchema[schema] = a
	r.order = append(r.order, schema)
	for _, d := range domains {
		if key := NormalizeDomain(d); key != "" {
			r.byDomain[key] = schema
		}
	}
}

func (r *Registry) Get(schema Schema) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if a, ok := r.bySchema[schema]; ok {
		return a, nil
	}
	return nil, &OpError{Op: "registry", Schema: schema, Kind: KindUnsupported}
}

func (r *Registry) Suggest(host string) (Schema, bool) {
	key := NormalizeDomain(host)
	if key == "" {
		return "", false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if s, ok := r.byDomain[key]; ok {
		return s, true
	}
	return "", false
}

func (r *Registry) List() []SchemaInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]SchemaInfo, 0, len(r.order))
	for _, schema := range r.order {
		a := r.bySchema[schema]
		var domains []string
		for d, s := range r.byDomain {
			if s == schema {
				domains = append(domains, d)
			}
		}
		sort.Strings(domains)
		out = append(out, SchemaInfo{
			Schema:       schema,
			DisplayName:  a.DisplayName(),
			Capabilities: a.Capabilities(),
			Defaults:     a.Defaults(),
			Domains:      domains,
		})
	}
	return out
}

func NormalizeDomain(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s[i:], "]") {
		s = s[:i]
	}
	s = strings.TrimPrefix(strings.ToLower(s), "www.")
	return s
}

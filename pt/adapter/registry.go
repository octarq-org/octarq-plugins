package adapter

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Info is the public description of a dialect, used by the add-tracker form
// and the capability matrix in the UI.
type Info struct {
	Schema       Schema       `json:"schema"`
	DisplayName  string       `json:"display_name"`
	Capabilities Capabilities `json:"capabilities"`
	Defaults     Defaults     `json:"defaults"`
	Domains      []string     `json:"domains,omitempty"`
}

// Registry holds the compiled-in dialects. It is filled once at construction
// and only read afterwards, so lookups are safe without a lock on the hot
// path; the mutex covers registration during init.
type Registry struct {
	mu       sync.RWMutex
	bySchema map[Schema]Adapter
	byDomain map[string]Schema
	order    []Schema
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		bySchema: map[Schema]Adapter{},
		byDomain: map[string]Schema{},
	}
}

// MustRegister adds a dialect and its domain hints. Duplicate schemas panic:
// two dialects claiming one schema would make behaviour depend on init order.
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

// Get returns the dialect for schema. An unknown schema is ErrUnsupported;
// callers must not fall back to another dialect, because a wrong dialect
// writes the wrong cookies and reports meaningless success.
func (r *Registry) Get(schema Schema) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if a, ok := r.bySchema[schema]; ok {
		return a, nil
	}
	return nil, &OpError{Op: "registry", Schema: schema, Kind: KindUnsupported}
}

// Suggest returns the domain hint for a host, if one is registered. It is a
// preselection for the form, never an automatic decision.
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

// List returns every dialect ordered by schema for a stable UI.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Info, 0, len(r.order))
	for _, schema := range r.order {
		a := r.bySchema[schema]
		var domains []string
		for d, s := range r.byDomain {
			if s == schema {
				domains = append(domains, d)
			}
		}
		sort.Strings(domains)
		out = append(out, Info{
			Schema:       schema,
			DisplayName:  a.DisplayName(),
			Capabilities: a.Capabilities(),
			Defaults:     a.Defaults(),
			Domains:      domains,
		})
	}
	return out
}

// NormalizeDomain lowercases a host or URL and strips the scheme, path, port,
// and a leading www.
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

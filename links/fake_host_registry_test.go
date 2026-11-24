package links

import (
	"context"
	"strings"
	"sync"

	"github.com/octarq-org/octarq/server/plugin"
)

type fakeBinding struct {
	orgID   uint
	traffic bool
}

type fakeHostRegistry struct {
	mu       sync.Mutex
	bindings map[string]map[string]fakeBinding
}

func newFakeHostRegistry() *fakeHostRegistry {
	return &fakeHostRegistry{
		bindings: make(map[string]map[string]fakeBinding),
	}
}

func (r *fakeHostRegistry) OwnerOf(host string) (uint, bool) {
	if r == nil {
		return 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	host = strings.ToLower(strings.TrimSpace(host))
	candidates := hostCandidates(host)
	for _, c := range candidates {
		sources := r.bindings[c]
		if len(sources) == 0 {
			continue
		}
		var owner uint
		ambiguous := false
		for _, b := range sources {
			if owner == 0 {
				owner = b.orgID
			} else if owner != b.orgID {
				ambiguous = true
				break
			}
		}
		if ambiguous {
			return 0, false
		}
		if owner != 0 {
			return owner, true
		}
	}
	return 0, false
}

func (r *fakeHostRegistry) ServesTraffic(host string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	host = strings.ToLower(strings.TrimSpace(host))
	candidates := hostCandidates(host)
	for _, c := range candidates {
		sources := r.bindings[c]
		for _, b := range sources {
			if b.traffic {
				return true
			}
		}
	}
	return false
}

func (r *fakeHostRegistry) Any() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bindings) > 0
}

func (r *fakeHostRegistry) Bind(ctx context.Context, orgID uint, host string, traffic bool, source string) error {
	if r == nil {
		return plugin.ErrHostsUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return plugin.ErrHostInvalid
	}
	if orgID == 0 {
		return plugin.ErrHostOrgRequired
	}
	if source == "" {
		return plugin.ErrHostSourceRequired
	}
	for s, b := range r.bindings[host] {
		if s != source && b.orgID != orgID {
			return plugin.ErrHostTaken
		}
	}
	if r.bindings[host] == nil {
		r.bindings[host] = make(map[string]fakeBinding)
	}
	r.bindings[host][source] = fakeBinding{orgID: orgID, traffic: traffic}
	return nil
}

func (r *fakeHostRegistry) Unbind(ctx context.Context, orgID uint, host, source string) error {
	if r == nil {
		return plugin.ErrHostsUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	host = strings.ToLower(strings.TrimSpace(host))
	if r.bindings[host] != nil {
		delete(r.bindings[host], source)
		if len(r.bindings[host]) == 0 {
			delete(r.bindings, host)
		}
	}
	return nil
}

func (r *fakeHostRegistry) ReplaceOwned(ctx context.Context, orgID uint, source string, hosts []string, traffic bool) error {
	if r == nil {
		return plugin.ErrHostsUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if orgID == 0 {
		return plugin.ErrHostOrgRequired
	}
	if source == "" {
		return plugin.ErrHostSourceRequired
	}
	normHosts := make([]string, 0, len(hosts))
	seen := make(map[string]bool)
	for _, raw := range hosts {
		h := strings.ToLower(strings.TrimSpace(raw))
		if h == "" {
			continue
		}
		if !seen[h] {
			seen[h] = true
			normHosts = append(normHosts, h)
		}
	}
	for _, h := range normHosts {
		for s, b := range r.bindings[h] {
			if s != source && b.orgID != orgID {
				return plugin.ErrHostTaken
			}
		}
	}
	for h, sources := range r.bindings {
		delete(sources, source)
		if len(sources) == 0 {
			delete(r.bindings, h)
		}
	}
	for _, h := range normHosts {
		if r.bindings[h] == nil {
			r.bindings[h] = make(map[string]fakeBinding)
		}
		r.bindings[h][source] = fakeBinding{orgID: orgID, traffic: traffic}
	}
	return nil
}

func (r *fakeHostRegistry) setAmbiguous(host string, traffic bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	host = strings.ToLower(strings.TrimSpace(host))
	if r.bindings[host] == nil {
		r.bindings[host] = make(map[string]fakeBinding)
	}
	r.bindings[host]["src1"] = fakeBinding{orgID: 1, traffic: traffic}
	r.bindings[host]["src2"] = fakeBinding{orgID: 2, traffic: traffic}
}

func hostCandidates(host string) []string {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return []string{host}
	}
	out := make([]string, 0, len(labels)-1)
	for i := 0; i+1 < len(labels); i++ {
		out = append(out, strings.Join(labels[i:], "."))
	}
	return out
}

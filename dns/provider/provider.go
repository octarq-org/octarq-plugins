package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

type Record struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Proxied  bool   `json:"proxied,omitempty"`
	Comment  string `json:"comment,omitempty"`
	Priority *int   `json:"priority,omitempty"`
}

type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Provider interface {
	ListZones(ctx context.Context) ([]Zone, error)
	ListRecords(ctx context.Context, zoneID string) ([]Record, error)
	CreateRecord(ctx context.Context, zoneID string, r Record) (Record, error)
	UpdateRecord(ctx context.Context, zoneID string, r Record) (Record, error)
	DeleteRecord(ctx context.Context, zoneID, recordID string) error
	VerifyZone(ctx context.Context, zoneID string) (string, error)
}

type Factory func(credsJSON []byte) (Provider, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

func Register(name string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = f
}

func New(name string, credsJSON []byte) (Provider, error) {
	registryMu.RLock()
	f, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown dns provider %q", name)
	}
	return f(credsJSON)
}

func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func MarshalCreds(v any) ([]byte, error) { return json.Marshal(v) }

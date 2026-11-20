package provider

import (
	"errors"
	"sort"
	"testing"
)

type dummyProvider struct{ Provider }

func TestRegister(t *testing.T) {
	dummyName := "test_register_provider"
	mockFactory := func(credsJSON []byte) (Provider, error) {
		return &dummyProvider{}, nil
	}

	Register(dummyName, mockFactory)

	registryMu.RLock()
	f, ok := registry[dummyName]
	registryMu.RUnlock()

	if !ok {
		t.Fatalf("expected provider %q to be registered", dummyName)
	}
	if f == nil {
		t.Fatal("expected non-nil factory to be registered")
	}
}

func TestRegisterAndNew(t *testing.T) {

	dummyName := "test_dummy_provider"

	Register(dummyName, func(credsJSON []byte) (Provider, error) {
		if string(credsJSON) == "error" {
			return nil, errors.New("simulated error")
		}
		return &dummyProvider{}, nil
	})

	p, err := New(dummyName, []byte("valid"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*dummyProvider); !ok {
		t.Fatalf("expected provider to be of type *dummyProvider, got %T", p)
	}

	_, err = New(dummyName, []byte("error"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "simulated error" {
		t.Fatalf("expected simulated error, got: %v", err)
	}

	p2, err := New("non_existent_provider_xyz123", nil)
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
	if p2 != nil {
		t.Fatalf("expected nil provider, got %T", p2)
	}
}

func TestProviderRegistryAndConstructors(t *testing.T) {
	t.Parallel()

	if _, err := New("unknown_prov", nil); err == nil {
		t.Error("expected error for unknown provider")
	}

	if _, err := New("cloudflare", []byte("bad json")); err == nil {
		t.Error("expected error for bad cloudflare json")
	}

	if _, err := New("cloudflare", []byte("{}")); err == nil {
		t.Error("expected error for missing apiToken")
	}

	p, err := New("cloudflare", []byte(`{"apiToken":"test-token"}`))
	if err != nil || p == nil {
		t.Errorf("cloudflare constructor failed: %v", err)
	}

	if _, err := New("dnspod", []byte("{}")); err == nil {
		t.Error("expected error for missing dnspod secrets")
	}

	p2, err := New("dnspod", []byte(`{"secretId":"id123","secretKey":"key456"}`))
	if err != nil || p2 == nil {
		t.Errorf("dnspod secretId/secretKey constructor failed: %v", err)
	}

	names := Names()
	if len(names) < 2 {
		t.Errorf("expected at least 2 registered providers, got %v", names)
	}
}

func TestMarshalCreds(t *testing.T) {
	t.Parallel()

	b, err := MarshalCreds(map[string]string{"apiToken": "xyz"})
	if err != nil || len(b) == 0 {
		t.Fatalf("MarshalCreds error: %v", err)
	}
	if string(b) != `{"apiToken":"xyz"}` {
		t.Errorf("unexpected marshaled result: %s", string(b))
	}

	type creds struct {
		Secret string `json:"secret"`
	}
	b2, err := MarshalCreds(creds{Secret: "foo"})
	if err != nil || len(b2) == 0 {
		t.Fatalf("MarshalCreds struct error: %v", err)
	}
	if string(b2) != `{"secret":"foo"}` {
		t.Errorf("unexpected marshaled result: %s", string(b2))
	}

	_, err = MarshalCreds(make(chan int))
	if err == nil {
		t.Error("expected error marshaling channel")
	}
}

func TestNames(t *testing.T) {

	Register("mock_c", func([]byte) (Provider, error) { return nil, nil })
	Register("mock_a", func([]byte) (Provider, error) { return nil, nil })
	Register("mock_b", func([]byte) (Provider, error) { return nil, nil })

	names := Names()

	if !sort.StringsAreSorted(names) {
		t.Errorf("expected Names() to return a sorted slice, got %v", names)
	}

	expectedMocks := []string{"mock_a", "mock_b", "mock_c"}
	for _, mock := range expectedMocks {
		found := false
		for _, name := range names {
			if name == mock {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected Names() to contain %q, but it was missing", mock)
		}
	}
}

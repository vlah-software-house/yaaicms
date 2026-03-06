// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package ai

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
)

// mockProvider is a test double implementing the Provider interface.
// It records calls and returns configurable responses.
type mockProvider struct {
	name       string
	response   string
	err        error
	callCount  int
	lastSystem string
	lastUser   string
	mu         sync.Mutex
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Generate(_ context.Context, systemPrompt, userPrompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	m.lastSystem = systemPrompt
	m.lastUser = userPrompt
	return m.response, m.err
}

func (m *mockProvider) GenerateWithModel(ctx context.Context, _, systemPrompt, userPrompt string) (string, error) {
	// For testing, delegate to Generate (ignores model).
	return m.Generate(ctx, systemPrompt, userPrompt)
}

// ---------- Registry.Generate ----------

func TestRegistryGenerate(t *testing.T) {
	t.Run("delegates to active provider", func(t *testing.T) {
		mock := &mockProvider{name: "test", response: "Hello from mock"}

		reg := &Registry{
			providers: map[string]Provider{"test": mock},
			active:    "test",
		}

		result, err := reg.Generate(context.Background(), "system", "user")
		if err != nil {
			t.Fatalf("Generate: unexpected error: %v", err)
		}
		if result != "Hello from mock" {
			t.Errorf("result: got %q, want %q", result, "Hello from mock")
		}

		mock.mu.Lock()
		defer mock.mu.Unlock()
		if mock.callCount != 1 {
			t.Errorf("callCount: got %d, want 1", mock.callCount)
		}
		if mock.lastSystem != "system" {
			t.Errorf("systemPrompt: got %q, want %q", mock.lastSystem, "system")
		}
		if mock.lastUser != "user" {
			t.Errorf("userPrompt: got %q, want %q", mock.lastUser, "user")
		}
	})

	t.Run("propagates provider error", func(t *testing.T) {
		mock := &mockProvider{name: "test", err: fmt.Errorf("api failure")}

		reg := &Registry{
			providers: map[string]Provider{"test": mock},
			active:    "test",
		}

		_, err := reg.Generate(context.Background(), "system", "user")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "api failure" {
			t.Errorf("error: got %q, want %q", err.Error(), "api failure")
		}
	})
}

func TestRegistryGenerateNoProvider(t *testing.T) {
	t.Run("error when no provider is active", func(t *testing.T) {
		reg := &Registry{
			providers: map[string]Provider{},
			active:    "nonexistent",
		}

		_, err := reg.Generate(context.Background(), "system", "user")
		if err == nil {
			t.Fatal("expected error when no provider is active, got nil")
		}
	})

	t.Run("error when active name does not match any registered provider", func(t *testing.T) {
		mock := &mockProvider{name: "openai", response: "hi"}

		reg := &Registry{
			providers: map[string]Provider{"openai": mock},
			active:    "gemini", // Not registered.
		}

		_, err := reg.Generate(context.Background(), "system", "user")
		if err == nil {
			t.Fatal("expected error for mismatched active provider, got nil")
		}
	})
}

// ---------- Registry.SetActive ----------

func TestRegistrySetActive(t *testing.T) {
	t.Run("switches to valid provider", func(t *testing.T) {
		mockA := &mockProvider{name: "a", response: "from a"}
		mockB := &mockProvider{name: "b", response: "from b"}

		reg := &Registry{
			providers: map[string]Provider{"a": mockA, "b": mockB},
			active:    "a",
		}

		if err := reg.SetActive("b"); err != nil {
			t.Fatalf("SetActive(b): unexpected error: %v", err)
		}
		if reg.ActiveName() != "b" {
			t.Errorf("ActiveName: got %q, want %q", reg.ActiveName(), "b")
		}

		// Verify Generate uses the new active provider.
		result, err := reg.Generate(context.Background(), "sys", "usr")
		if err != nil {
			t.Fatalf("Generate: unexpected error: %v", err)
		}
		if result != "from b" {
			t.Errorf("result: got %q, want %q", result, "from b")
		}
	})

	t.Run("can switch back to original provider", func(t *testing.T) {
		mockA := &mockProvider{name: "a", response: "from a"}
		mockB := &mockProvider{name: "b", response: "from b"}

		reg := &Registry{
			providers: map[string]Provider{"a": mockA, "b": mockB},
			active:    "a",
		}

		_ = reg.SetActive("b")
		_ = reg.SetActive("a")

		if reg.ActiveName() != "a" {
			t.Errorf("ActiveName: got %q, want %q", reg.ActiveName(), "a")
		}
	})
}

func TestRegistrySetActiveInvalid(t *testing.T) {
	t.Run("returns error for non-existent provider", func(t *testing.T) {
		mock := &mockProvider{name: "openai", response: "hi"}

		reg := &Registry{
			providers: map[string]Provider{"openai": mock},
			active:    "openai",
		}

		err := reg.SetActive("nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent provider, got nil")
		}

		// Active provider should not have changed.
		if reg.ActiveName() != "openai" {
			t.Errorf("ActiveName should remain %q, got %q", "openai", reg.ActiveName())
		}
	})

	t.Run("returns error for empty name", func(t *testing.T) {
		mock := &mockProvider{name: "openai", response: "hi"}

		reg := &Registry{
			providers: map[string]Provider{"openai": mock},
			active:    "openai",
		}

		err := reg.SetActive("")
		if err == nil {
			t.Fatal("expected error for empty provider name, got nil")
		}
	})
}

// ---------- Registry.Available ----------

func TestRegistryAvailable(t *testing.T) {
	t.Run("returns all registered providers", func(t *testing.T) {
		reg := &Registry{
			providers: map[string]Provider{
				"openai":  &mockProvider{name: "openai"},
				"gemini":  &mockProvider{name: "gemini"},
				"mistral": &mockProvider{name: "mistral"},
			},
			active: "openai",
		}

		available := reg.Available()
		if len(available) != 3 {
			t.Fatalf("len(Available): got %d, want 3", len(available))
		}

		sort.Strings(available)
		want := []string{"gemini", "mistral", "openai"}
		for i, name := range available {
			if name != want[i] {
				t.Errorf("Available[%d]: got %q, want %q", i, name, want[i])
			}
		}
	})

	t.Run("returns empty slice when no providers", func(t *testing.T) {
		reg := &Registry{
			providers: map[string]Provider{},
			active:    "none",
		}

		available := reg.Available()
		if len(available) != 0 {
			t.Errorf("len(Available): got %d, want 0", len(available))
		}
	})

	t.Run("single provider", func(t *testing.T) {
		reg := &Registry{
			providers: map[string]Provider{
				"claude": &mockProvider{name: "claude"},
			},
			active: "claude",
		}

		available := reg.Available()
		if len(available) != 1 {
			t.Fatalf("len(Available): got %d, want 1", len(available))
		}
		if available[0] != "claude" {
			t.Errorf("Available[0]: got %q, want %q", available[0], "claude")
		}
	})
}

// ---------- Registry.HasProvider ----------

func TestRegistryHasProvider(t *testing.T) {
	reg := &Registry{
		providers: map[string]Provider{
			"openai": &mockProvider{name: "openai"},
			"gemini": &mockProvider{name: "gemini"},
		},
		active: "openai",
	}

	tests := []struct {
		name string
		want bool
	}{
		{"openai", true},
		{"gemini", true},
		{"claude", false},
		{"mistral", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reg.HasProvider(tt.name)
			if got != tt.want {
				t.Errorf("HasProvider(%q): got %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// ---------- Concurrency ----------

func TestRegistryConcurrency(t *testing.T) {
	t.Run("concurrent SetActive and Active are safe", func(t *testing.T) {
		mockA := &mockProvider{name: "a", response: "from a"}
		mockB := &mockProvider{name: "b", response: "from b"}

		reg := &Registry{
			providers: map[string]Provider{"a": mockA, "b": mockB},
			active:    "a",
		}

		const goroutines = 100
		var wg sync.WaitGroup
		wg.Add(goroutines * 3) // SetActive writers + Active readers + Generate readers

		// Writers: toggle between providers.
		for i := 0; i < goroutines; i++ {
			go func(i int) {
				defer wg.Done()
				name := "a"
				if i%2 == 0 {
					name = "b"
				}
				_ = reg.SetActive(name)
			}(i)
		}

		// Readers: read the active provider name.
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				name := reg.ActiveName()
				if name != "a" && name != "b" {
					t.Errorf("unexpected active name: %q", name)
				}
			}()
		}

		// Readers: call Generate.
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				result, err := reg.Generate(context.Background(), "sys", "usr")
				if err != nil {
					t.Errorf("Generate error during concurrency: %v", err)
					return
				}
				if result != "from a" && result != "from b" {
					t.Errorf("unexpected result: %q", result)
				}
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent Available and HasProvider are safe", func(t *testing.T) {
		reg := &Registry{
			providers: map[string]Provider{
				"openai":  &mockProvider{name: "openai"},
				"gemini":  &mockProvider{name: "gemini"},
				"claude":  &mockProvider{name: "claude"},
				"mistral": &mockProvider{name: "mistral"},
			},
			active: "openai",
		}

		const goroutines = 50
		var wg sync.WaitGroup
		wg.Add(goroutines * 2)

		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				avail := reg.Available()
				if len(avail) != 4 {
					t.Errorf("Available: got %d providers, want 4", len(avail))
				}
			}()
		}

		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				if !reg.HasProvider("openai") {
					t.Error("HasProvider(openai) should be true")
				}
			}()
		}

		wg.Wait()
	})
}

// ---------- NewRegistry (Provider Constructors via Name) ----------

func TestNewRegistryProviderNames(t *testing.T) {
	tests := []struct {
		providerName string
		wantName     string
	}{
		{"openai", "openai"},
		{"gemini", "gemini"},
		{"claude", "claude"},
		{"mistral", "mistral"},
	}

	for _, tt := range tests {
		t.Run(tt.providerName, func(t *testing.T) {
			reg := NewRegistry(tt.providerName, map[string]ProviderConfig{
				tt.providerName: {APIKey: "test-key", Model: "test-model"},
			})

			p, err := reg.Active()
			if err != nil {
				t.Fatalf("Active: unexpected error: %v", err)
			}
			if p.Name() != tt.wantName {
				t.Errorf("Name: got %q, want %q", p.Name(), tt.wantName)
			}
		})
	}
}

func TestNewRegistrySkipsEmptyAPIKey(t *testing.T) {
	reg := NewRegistry("openai", map[string]ProviderConfig{
		"openai":  {APIKey: "", Model: "gpt-4o"},
		"gemini":  {APIKey: "valid-key", Model: "gemini-pro"},
		"claude":  {APIKey: "", Model: "claude-sonnet"},
		"mistral": {APIKey: "", Model: "mistral-large"},
	})

	if reg.HasProvider("openai") {
		t.Error("openai should be skipped (no API key)")
	}
	if !reg.HasProvider("gemini") {
		t.Error("gemini should be available (has API key)")
	}
	if reg.HasProvider("claude") {
		t.Error("claude should be skipped (no API key)")
	}
	if reg.HasProvider("mistral") {
		t.Error("mistral should be skipped (no API key)")
	}

	available := reg.Available()
	if len(available) != 1 {
		t.Errorf("len(Available): got %d, want 1", len(available))
	}
}

func TestNewRegistryIgnoresUnknownProvider(t *testing.T) {
	reg := NewRegistry("unknown", map[string]ProviderConfig{
		"unknown": {APIKey: "key", Model: "model"},
	})

	if reg.HasProvider("unknown") {
		t.Error("unknown provider should not be registered")
	}

	available := reg.Available()
	if len(available) != 0 {
		t.Errorf("len(Available): got %d, want 0", len(available))
	}
}

// ---------- Registry.Active ----------

func TestRegistryActive(t *testing.T) {
	t.Run("returns active provider", func(t *testing.T) {
		mock := &mockProvider{name: "openai"}
		reg := &Registry{
			providers: map[string]Provider{"openai": mock},
			active:    "openai",
		}

		p, err := reg.Active()
		if err != nil {
			t.Fatalf("Active: unexpected error: %v", err)
		}
		if p.Name() != "openai" {
			t.Errorf("Name: got %q, want %q", p.Name(), "openai")
		}
	})

	t.Run("returns error when active not found", func(t *testing.T) {
		reg := &Registry{
			providers: map[string]Provider{},
			active:    "missing",
		}

		_, err := reg.Active()
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

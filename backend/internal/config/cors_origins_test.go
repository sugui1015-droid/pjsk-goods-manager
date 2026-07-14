package config

import (
	"strings"
	"testing"
)

func TestLoadCORSAllowedOriginsDefaults(t *testing.T) {
	viteDefaults := []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	tests := []struct {
		name           string
		appEnvironment string
		want           []string
	}{
		{name: "development", appEnvironment: "development", want: viteDefaults},
		{name: "test", appEnvironment: "test", want: viteDefaults},
		{name: "empty env", appEnvironment: "", want: viteDefaults},
		{name: "production", appEnvironment: "production", want: nil},
		{name: "production padded case", appEnvironment: " PrOdUcTiOn ", want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CORS_ALLOWED_ORIGINS", "")
			origins, err := loadCORSAllowedOrigins(test.appEnvironment)
			if err != nil {
				t.Fatal(err)
			}
			if len(origins) != len(test.want) {
				t.Fatalf("origins = %v, want %v", origins, test.want)
			}
			for i, want := range test.want {
				if origins[i] != want {
					t.Fatalf("origins[%d] = %q, want %q", i, origins[i], want)
				}
			}
		})
	}
}

func TestLoadCORSAllowedOriginsValidValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "http origin", value: "http://intranet-frontend.internal.example", want: []string{"http://intranet-frontend.internal.example"}},
		{name: "https origin", value: "https://frontend.internal.example", want: []string{"https://frontend.internal.example"}},
		{name: "with port", value: "https://frontend.internal.example:8443", want: []string{"https://frontend.internal.example:8443"}},
		{name: "IPv6 literal", value: "http://[::1]:5173", want: []string{"http://[::1]:5173"}},
		{name: "trailing slash normalized", value: "https://frontend.internal.example/", want: []string{"https://frontend.internal.example"}},
		{name: "scheme and host lowercased", value: "HTTPS://Frontend.Internal.Example", want: []string{"https://frontend.internal.example"}},
		{name: "multiple", value: "https://a.internal.example, http://b.internal.example:8080", want: []string{"https://a.internal.example", "http://b.internal.example:8080"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CORS_ALLOWED_ORIGINS", test.value)
			origins, err := loadCORSAllowedOrigins("production")
			if err != nil {
				t.Fatal(err)
			}
			if len(origins) != len(test.want) {
				t.Fatalf("origins = %v, want %v", origins, test.want)
			}
			for i, want := range test.want {
				if origins[i] != want {
					t.Fatalf("origins[%d] = %q, want %q", i, origins[i], want)
				}
			}
		})
	}
}

func TestLoadCORSAllowedOriginsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "wildcard", value: "*"},
		{name: "null", value: "null"},
		{name: "path", value: "https://frontend.internal.example/app"},
		{name: "query", value: "https://frontend.internal.example?x=1"},
		{name: "fragment", value: "https://frontend.internal.example#frag"},
		{name: "username", value: "https://user@frontend.internal.example"},
		{name: "password", value: "https://user:secret@frontend.internal.example"},
		{name: "empty middle item", value: "https://a.internal.example,,https://b.internal.example"},
		{name: "duplicate after normalization", value: "https://a.internal.example,HTTPS://A.Internal.Example/"},
		{name: "no scheme", value: "frontend.internal.example"},
		{name: "unsupported scheme", value: "ftp://frontend.internal.example"},
		{name: "opaque", value: "mailto:user@example.com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CORS_ALLOWED_ORIGINS", test.value)
			_, err := loadCORSAllowedOrigins("production")
			if err == nil {
				t.Fatalf("value was accepted: %s", test.name)
			}
			if !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS") {
				t.Fatal("error did not identify the variable")
			}
			if strings.Contains(err.Error(), "internal.example") || strings.Contains(err.Error(), "secret") {
				t.Fatal("error echoed the configured origin value")
			}
		})
	}
}

// Exact-match semantics: lookalike origins must remain distinct strings
// after normalization so the middleware's exact comparison rejects them.
func TestLoadCORSAllowedOriginsKeepsExactOriginsOnly(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://allowed.example")
	origins, err := loadCORSAllowedOrigins("production")
	if err != nil {
		t.Fatal(err)
	}
	for _, attack := range []string{"https://allowed.example.evil", "https://allowed.example:8443", "http://allowed.example"} {
		if origins[0] == attack {
			t.Fatalf("normalization collapsed %q into the allowed origin", attack)
		}
	}
}

package config

import (
	"net/netip"
	"strings"
	"testing"
)

func TestLoadTrustedProxyCIDRsEmpty(t *testing.T) {
	for name, value := range map[string]string{"unset": "", "blank": "   "} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXY_CIDRS", value)
			prefixes, err := loadTrustedProxyCIDRs()
			if err != nil || len(prefixes) != 0 {
				t.Fatalf("expected empty trusted list, got %v, err %v", prefixes, err)
			}
		})
	}
}

func TestLoadTrustedProxyCIDRsValidEntries(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "single IPv4", value: "127.0.0.1/32", want: []string{"127.0.0.1/32"}},
		{name: "single IPv6", value: "::1/128", want: []string{"::1/128"}},
		{name: "multiple", value: " 127.0.0.1/32 , ::1/128 , 10.0.0.0/8 ", want: []string{"127.0.0.1/32", "::1/128", "10.0.0.0/8"}},
		{name: "duplicates deduplicated", value: "127.0.0.1/32,127.0.0.1/32", want: []string{"127.0.0.1/32"}},
		{name: "unmasked bits normalized", value: "10.1.2.3/8,10.0.0.0/8", want: []string{"10.0.0.0/8"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXY_CIDRS", test.value)
			prefixes, err := loadTrustedProxyCIDRs()
			if err != nil {
				t.Fatal(err)
			}
			if len(prefixes) != len(test.want) {
				t.Fatalf("got %d prefixes %v, want %d", len(prefixes), prefixes, len(test.want))
			}
			for i, want := range test.want {
				if prefixes[i] != netip.MustParsePrefix(want) {
					t.Fatalf("prefix %d = %v, want %s", i, prefixes[i], want)
				}
			}
		})
	}
}

func TestLoadTrustedProxyCIDRsRejectsInvalidEntries(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty middle item", value: "127.0.0.1/32,,::1/128"},
		{name: "bare IPv4", value: "127.0.0.1"},
		{name: "bare IPv6", value: "::1"},
		{name: "hostname", value: "proxy.internal.example/32"},
		{name: "invalid mask", value: "127.0.0.1/33"},
		{name: "IPv4 whole space", value: "0.0.0.0/0"},
		{name: "IPv6 whole space", value: "::/0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXY_CIDRS", test.value)
			_, err := loadTrustedProxyCIDRs()
			if err == nil {
				t.Fatalf("value %q was accepted", test.name)
			}
			if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
				t.Fatal("error did not identify the variable")
			}
			if strings.Contains(err.Error(), test.value) {
				t.Fatal("error echoed the full configured value")
			}
		})
	}
}

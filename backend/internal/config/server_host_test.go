package config

import (
	"net"
	"strings"
	"testing"
)

func TestLoadServerHostDefaultsToLoopback(t *testing.T) {
	for name, value := range map[string]string{"unset": "", "blank": "   "} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SERVER_HOST", value)
			host, err := loadServerHost("development")
			if err != nil || host != "127.0.0.1" {
				t.Fatalf("host = %q, err %v; want 127.0.0.1", host, err)
			}
		})
	}
}

func TestLoadServerHostAcceptsLiteralAddresses(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "IPv4", value: "192.0.2.10", want: "192.0.2.10"},
		{name: "IPv6", value: "2001:db8::5", want: "2001:db8::5"},
		{name: "IPv6 loopback", value: "::1", want: "::1"},
		{name: "IPv4-mapped IPv6 unmapped", value: "::ffff:192.0.2.10", want: "192.0.2.10"},
		{name: "explicit all IPv4", value: "0.0.0.0", want: "0.0.0.0"},
		{name: "explicit all IPv6", value: "::", want: "::"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SERVER_HOST", test.value)
			host, err := loadServerHost("development")
			if err != nil || host != test.want {
				t.Fatalf("host = %q, err %v; want %q", host, err, test.want)
			}
		})
	}
}

func TestLoadServerHostRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "hostname", value: "localhost"},
		{name: "dns name", value: "backend.internal.example"},
		{name: "IPv4 with port", value: "127.0.0.1:8080"},
		{name: "IPv6 with port", value: "[::1]:8080"},
		{name: "CIDR", value: "10.0.0.0/8"},
		{name: "zoned", value: "fe80::1%eth0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SERVER_HOST", test.value)
			_, err := loadServerHost("development")
			if err == nil {
				t.Fatalf("value %q was accepted", test.value)
			}
			if !strings.Contains(err.Error(), "SERVER_HOST") {
				t.Fatal("error did not identify the variable")
			}
		})
	}
}

func TestLoadServerHostProductionWildcardWarning(t *testing.T) {
	t.Setenv("SERVER_HOST", "0.0.0.0")
	warning := captureConfigLog(t, func() {
		host, err := loadServerHost(" Production ")
		if err != nil || host != "0.0.0.0" {
			t.Fatalf("host = %q, err %v", host, err)
		}
	})
	if !strings.Contains(warning, "SERVER_HOST") || !strings.Contains(warning, "all-interfaces") {
		t.Fatalf("expected a production all-interfaces warning, got %q", warning)
	}
	for _, sensitive := range []string{"DATABASE", "SMTP", "KEY", "PASSWORD"} {
		if strings.Contains(warning, sensitive) {
			t.Fatalf("warning leaked unrelated configuration: %q", warning)
		}
	}

	// Loopback in production must not warn.
	t.Setenv("SERVER_HOST", "127.0.0.1")
	warning = captureConfigLog(t, func() {
		if _, err := loadServerHost("production"); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(warning, "all-interfaces") {
		t.Fatal("loopback bind must not trigger the wildcard warning")
	}
}

func TestLoadPortPrecedenceUnchanged(t *testing.T) {
	clear := func() {
		t.Setenv("APP_PORT", "")
		t.Setenv("SERVER_PORT", "")
		t.Setenv("BACKEND_PORT", "")
	}

	clear()
	if port := loadPort(); port != "8080" {
		t.Fatalf("default port = %q, want 8080", port)
	}

	clear()
	t.Setenv("BACKEND_PORT", "9002")
	if port := loadPort(); port != "9002" {
		t.Fatalf("BACKEND_PORT fallback = %q", port)
	}

	t.Setenv("SERVER_PORT", "9001")
	if port := loadPort(); port != "9001" {
		t.Fatalf("SERVER_PORT should win over BACKEND_PORT, got %q", port)
	}

	t.Setenv("APP_PORT", "9000")
	if port := loadPort(); port != "9000" {
		t.Fatalf("APP_PORT should win over SERVER_PORT, got %q", port)
	}
}

func TestServerHostJoinsWithPortForIPv6(t *testing.T) {
	t.Setenv("SERVER_HOST", "::1")
	host, err := loadServerHost("development")
	if err != nil {
		t.Fatal(err)
	}
	if joined := net.JoinHostPort(host, "8080"); joined != "[::1]:8080" {
		t.Fatalf("JoinHostPort = %q, want [::1]:8080", joined)
	}
}

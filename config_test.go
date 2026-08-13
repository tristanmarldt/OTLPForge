package main

import "testing"

func TestRuntimeConfigUsesEnvEndpointAndToken(t *testing.T) {
	t.Setenv(envEndpoint, "https://env.example/api/v2/otlp")
	t.Setenv(envToken, "env-token")

	cfg := Config{}
	runtimeCfg := cfg.runtimeConfig()

	if runtimeCfg.Endpoint != "https://env.example/api/v2/otlp" {
		t.Fatalf("expected endpoint from env, got %q", runtimeCfg.Endpoint)
	}
	if runtimeCfg.Token != "env-token" {
		t.Fatalf("expected token from env, got %q", runtimeCfg.Token)
	}
}

func TestDefaultConfigStartsWithoutCustomAttributes(t *testing.T) {
	cfg := defaultConfig()
	if len(cfg.Attributes) != 0 {
		t.Fatalf("default global attributes = %v, want none", cfg.Attributes)
	}
	if got := cfg.Services[0].Attributes; len(got) != 0 {
		t.Fatalf("default service attributes = %v, want none", got)
	}
}

func TestRuntimeConfigEnvOverridesSavedValues(t *testing.T) {
	t.Setenv(envEndpoint, "https://env.example/api/v2/otlp")
	t.Setenv(envToken, "env-token")

	cfg := Config{
		Endpoint: "https://saved.example/api/v2/otlp",
		Token:    "saved-token",
	}
	runtimeCfg := cfg.runtimeConfig()

	if runtimeCfg.Endpoint != "https://env.example/api/v2/otlp" {
		t.Fatalf("expected endpoint override from env, got %q", runtimeCfg.Endpoint)
	}
	if runtimeCfg.Token != "env-token" {
		t.Fatalf("expected token override from env, got %q", runtimeCfg.Token)
	}
}

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

package main

import "testing"

func TestRuntimeConfigUsesEnvFallbacks(t *testing.T) {
	t.Setenv(envEndpoint, "https://env.example/api/v2/otlp")
	t.Setenv(envToken, "env-token")
	t.Setenv(envTokenHeader, "Authorization")

	cfg := AppConfig{}
	runtimeCfg := cfg.runtimeConfig()

	if runtimeCfg.Endpoint != "https://env.example/api/v2/otlp" {
		t.Fatalf("expected endpoint from env, got %q", runtimeCfg.Endpoint)
	}
	if runtimeCfg.Token != "env-token" {
		t.Fatalf("expected token from env, got %q", runtimeCfg.Token)
	}
	if runtimeCfg.TokenHeader != "Authorization" {
		t.Fatalf("expected token header from env, got %q", runtimeCfg.TokenHeader)
	}
}

func TestRuntimeConfigEnvOverridesSavedValues(t *testing.T) {
	t.Setenv(envEndpoint, "https://env.example/api/v2/otlp")
	t.Setenv(envToken, "env-token")
	t.Setenv(envTokenHeader, "X-Test-Token")

	cfg := AppConfig{
		Endpoint:    "https://saved.example/api/v2/otlp",
		Token:       "saved-token",
		TokenHeader: "Authorization",
	}
	runtimeCfg := cfg.runtimeConfig()

	if runtimeCfg.Endpoint != "https://env.example/api/v2/otlp" {
		t.Fatalf("expected endpoint override from env, got %q", runtimeCfg.Endpoint)
	}
	if runtimeCfg.Token != "env-token" {
		t.Fatalf("expected token override from env, got %q", runtimeCfg.Token)
	}
	if runtimeCfg.TokenHeader != "X-Test-Token" {
		t.Fatalf("expected token header override from env, got %q", runtimeCfg.TokenHeader)
	}
}

func TestWithPreservedSecretKeepsExistingToken(t *testing.T) {
	current := AppConfig{Token: "saved-token"}
	next := AppConfig{Endpoint: "https://saved.example/api/v2/otlp"}

	merged := current.withPreservedSecret(next)
	if merged.Token != "saved-token" {
		t.Fatalf("expected saved token to be preserved, got %q", merged.Token)
	}
}

func TestNormalizeConfigClearsLegacyExampleEndpoint(t *testing.T) {
	normalized := normalizeConfig(AppConfig{Endpoint: legacyExampleEndpoint})

	if normalized.Endpoint != "" {
		t.Fatalf("expected legacy example endpoint to be cleared, got %q", normalized.Endpoint)
	}
}

func TestWithPreservedSecretAllowsExplicitTokenUpdate(t *testing.T) {
	current := AppConfig{Token: "saved-token"}
	next := AppConfig{Token: "new-token"}

	merged := current.withPreservedSecret(next)
	if merged.Token != "new-token" {
		t.Fatalf("expected explicit token update, got %q", merged.Token)
	}
}

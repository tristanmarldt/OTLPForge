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

func TestWithPreservedSecretKeepsAndAllowsToken(t *testing.T) {
	current := Config{Token: "saved-token"}

	// blank next.Token preserves current
	next := Config{Endpoint: "https://example.com"}
	merged := current.withPreservedSecret(next)
	if merged.Token != "saved-token" {
		t.Fatalf("expected saved token to be preserved, got %q", merged.Token)
	}

	// explicit next.Token is used
	next2 := Config{Token: "new-token"}
	merged2 := current.withPreservedSecret(next2)
	if merged2.Token != "new-token" {
		t.Fatalf("expected explicit token update, got %q", merged2.Token)
	}
}

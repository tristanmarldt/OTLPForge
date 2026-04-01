package main

import (
	"testing"

	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestBuildPayloadCreatesParentChildFailureTrace(t *testing.T) {
	cfg := normalizeConfig(AppConfig{
		EmitSpans:          true,
		ResourceAttributes: map[string]string{"service.name": "otlpforge"},
		SpanName:           "request",
		SpanKind:           "server",
		SpanMinDurationMs:  10,
		SpanMaxDurationMs:  10,
		SpanFailureRate:    100,
		SpanFailureMode:    "http",
		SpanFailureCode:    503,
		SpanFailureMessage: "upstream failure",
		SpanChildCount:     2,
	})

	payload, err := buildPayload(cfg, signalSpans)
	if err != nil {
		t.Fatalf("buildPayload returned error: %v", err)
	}

	var req collectortracepb.ExportTraceServiceRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		t.Fatalf("failed to unmarshal trace payload: %v", err)
	}

	resourceSpans := req.GetResourceSpans()
	if len(resourceSpans) != 1 {
		t.Fatalf("expected 1 resource span, got %d", len(resourceSpans))
	}

	spans := resourceSpans[0].GetScopeSpans()[0].GetSpans()
	if len(spans) != 3 {
		t.Fatalf("expected root + 2 child spans, got %d", len(spans))
	}

	root := spans[0]
	if root.GetKind() != tracepb.Span_SPAN_KIND_SERVER {
		t.Fatalf("expected server span kind, got %v", root.GetKind())
	}
	if root.GetStatus().GetCode() != tracepb.Status_STATUS_CODE_ERROR {
		t.Fatalf("expected failed root span, got %v", root.GetStatus().GetCode())
	}

	lastChild := spans[len(spans)-1]
	if string(lastChild.GetParentSpanId()) != string(root.GetSpanId()) {
		t.Fatalf("expected child parent span id to match root")
	}
	if lastChild.GetStatus().GetCode() != tracepb.Status_STATUS_CODE_ERROR {
		t.Fatalf("expected last child to fail, got %v", lastChild.GetStatus().GetCode())
	}
}

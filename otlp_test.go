package main

import (
	"strings"
	"testing"

	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestBuildPayloadCreatesSpanForService(t *testing.T) {
	cfg := Config{Endpoint: "https://example.com"}
	svc := Service{
		Name:        "svc",
		SpanKind:    "server",
		FailureRate: 100,
		Signals:     []string{"spans"},
		Attributes: map[string]AttrValue{
			"mystr":  strAttrVal("hello"),
			"mybool": boolAttrVal(true),
		},
	}

	payload, err := buildPayload(cfg, svc, signalSpans)
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
	if len(spans) != 1 {
		t.Fatalf("expected 1 span (no children), got %d", len(spans))
	}

	span := spans[0]
	if span.GetKind() != tracepb.Span_SPAN_KIND_SERVER {
		t.Fatalf("expected server span kind, got %v", span.GetKind())
	}
	if span.GetStatus().GetCode() != tracepb.Status_STATUS_CODE_ERROR {
		t.Fatalf("expected error status (failureRate=100), got %v", span.GetStatus().GetCode())
	}

	// Check resource has service.name="svc"
	resource := resourceSpans[0].GetResource()
	found := false
	for _, attr := range resource.GetAttributes() {
		if attr.GetKey() == "service.name" {
			if sv := attr.GetValue().GetStringValue(); sv == "svc" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected resource attribute service.name=svc")
	}
}

// TestK8sInfraTemplatesCarryDynatraceAttributes checks that every k8s-family
// template emits the attribute set Dynatrace's k8sattributesprocessor extracts
// and the Dynatrace Operator injects, so the payload maps onto the same
// Kubernetes entities as a real in-cluster collector would produce.
func TestK8sInfraTemplatesCarryDynatraceAttributes(t *testing.T) {
	required := []string{
		"k8s.cluster.name", "k8s.cluster.uid",
		"k8s.namespace.name", "k8s.node.name",
		"k8s.pod.name", "k8s.pod.uid", "k8s.pod.ip",
		"k8s.container.name",
		"k8s.deployment.name", "k8s.replicaset.name",
		"k8s.workload.kind", "k8s.workload.name",
	}

	for _, template := range []string{"k8s", "eks", "gke", "aks", "openshift"} {
		t.Run(template, func(t *testing.T) {
			attrs := infraDefaults(Service{Name: "svc", InfraTemplate: template})
			for _, key := range required {
				v, ok := attrs[key]
				if !ok {
					t.Errorf("missing %s", key)
					continue
				}
				if v.Type != "string" || v.Str == "" {
					t.Errorf("%s should be a non-empty string, got %+v", key, v)
				}
			}
			// Deployment → ReplicaSet → Pod names must stay consistent.
			rs := attrs["k8s.replicaset.name"].Str
			if !strings.HasPrefix(rs, attrs["k8s.deployment.name"].Str+"-") {
				t.Errorf("replicaset %q is not derived from deployment %q", rs, attrs["k8s.deployment.name"].Str)
			}
			if pod := attrs["k8s.pod.name"].Str; !strings.HasPrefix(pod, rs+"-") {
				t.Errorf("pod %q is not derived from replicaset %q", pod, rs)
			}
			if kind := attrs["k8s.workload.kind"].Str; kind != "Deployment" {
				t.Errorf("k8s.workload.kind = %q, want Deployment", kind)
			}
		})
	}
}

// TestTemplateSpanAttributes verifies that each template produces the expected
// semantic-convention attributes on the root span.
func TestTemplateSpanAttributes(t *testing.T) {
	cfg := Config{Endpoint: "https://example.com"}

	cases := []struct {
		template    string
		wantAttrKey string // one mandatory attribute key per template
	}{
		{"http-server", "http.request.method"},
		{"http-client", "url.full"},
		{"db", "db.system.name"},
		{"messaging", "messaging.system"},
		{"grpc", "rpc.system"},
	}

	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			svc := Service{
				Name:     "tsvc",
				Template: tc.template,
				SpanKind: "client",
				Signals:  []string{"spans"},
			}
			payload, err := buildPayload(cfg, svc, signalSpans)
			if err != nil {
				t.Fatalf("buildPayload: %v", err)
			}
			var req collectortracepb.ExportTraceServiceRequest
			if err := proto.Unmarshal(payload, &req); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			spans := req.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()
			if len(spans) == 0 {
				t.Fatal("expected at least one span")
			}
			span := spans[0]
			// span name must NOT be the generic "<svc>.request"
			if strings.HasSuffix(span.GetName(), ".request") {
				t.Errorf("expected template span name, got %q", span.GetName())
			}
			// must contain the template-specific attribute
			found := false
			for _, attr := range span.GetAttributes() {
				if attr.GetKey() == tc.wantAttrKey {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected span attribute %q for template %q", tc.wantAttrKey, tc.template)
			}
		})
	}
}

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mathrand "math/rand/v2"
	"strings"
	"time"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func buildPayload(cfg Config, svc Service, kind signalKind) ([]byte, error) {
	now := time.Now()
	resource := &resourcepb.Resource{Attributes: svcAttributes(svc)}
	scope := &commonpb.InstrumentationScope{Name: "otgen", Version: "1.0.0"}

	switch kind {
	case signalSpans:
		traceID := mustDecodeHex(randomHex(16), 16)
		rootID := mustDecodeHex(randomHex(8), 8)
		failed := mathrand.IntN(100) < svc.FailureRate
		rootDur := randomRootDuration()
		rootEnd := now.Add(rootDur)
		root := newSpan(svc, traceID, rootID, now, rootEnd, failed)

		spans := []*tracepb.Span{root}
		offset := 5 * time.Millisecond
		for i := 0; i < svc.ChildSpans; i++ {
			childID := mustDecodeHex(randomHex(8), 8)
			childDur := randomChildDuration()
			childStart := now.Add(offset)
			childEnd := childStart.Add(childDur)
			if childEnd.After(rootEnd) {
				childEnd = rootEnd
			}
			spans = append(spans, newChildSpan(svc.Name, traceID, childID, rootID, childStart, childEnd, i, failed))
			offset += childDur + 2*time.Millisecond
		}

		return proto.Marshal(&collectortracepb.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{{
				Resource:   resource,
				ScopeSpans: []*tracepb.ScopeSpans{{Scope: scope, Spans: spans}},
			}},
		})
	case signalMetrics:
		return proto.Marshal(&collectormetricspb.ExportMetricsServiceRequest{
			ResourceMetrics: []*metricspb.ResourceMetrics{{
				Resource: resource,
				ScopeMetrics: []*metricspb.ScopeMetrics{{
					Scope: scope,
					Metrics: []*metricspb.Metric{{
						Name: svc.Name + ".requests.total",
						Unit: "1",
						Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{
							AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
							IsMonotonic:            true,
							DataPoints: []*metricspb.NumberDataPoint{{
								TimeUnixNano: uint64(now.UnixNano()),
								Value:        &metricspb.NumberDataPoint_AsInt{AsInt: int64(now.UnixNano()%7 + 1)},
							}},
						}},
					}},
				}},
			}},
		})
	case signalLogs:
		return proto.Marshal(&collectorlogspb.ExportLogsServiceRequest{
			ResourceLogs: []*logspb.ResourceLogs{{
				Resource: resource,
				ScopeLogs: []*logspb.ScopeLogs{{
					Scope: scope,
					LogRecords: []*logspb.LogRecord{{
						TimeUnixNano:   uint64(now.UnixNano()),
						SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_INFO,
						SeverityText:   "INFO",
						Body:           &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: svc.Name + " synthetic log"}},
					}},
				}},
			}},
		})
	default:
		return nil, fmt.Errorf("unsupported signal type: %s", kind)
	}
}

// svcAttributes builds the resource attribute list for a service.
// Precedence: service.name (fixed) > svc.Attributes (user) > infraDefaults (template).
func svcAttributes(svc Service) []*commonpb.KeyValue {
	merged := infraDefaults(svc)
	if merged == nil {
		merged = make(map[string]AttrValue, len(svc.Attributes)+1)
	}
	for k, v := range svc.Attributes {
		merged[k] = v
	}
	merged["service.name"] = strAttrVal(svc.Name) // always wins
	return toOTLPAttributes(merged)
}

// infraDefaults returns resource attributes for the service's InfraTemplate.
// Values are deterministic so resource attributes stay stable across ticks.
// Users override specific values via svc.Attributes.
func infraDefaults(svc Service) map[string]AttrValue {
	name := svc.Name
	switch svc.InfraTemplate {

	case "k8s":
		return map[string]AttrValue{
			"k8s.cluster.name":    strAttrVal("my-cluster"),
			"k8s.namespace.name":  strAttrVal("default"),
			"k8s.pod.name":        strAttrVal(name + "-7d9f8b6c4-xzpqr"),
			"k8s.node.name":       strAttrVal("node-1"),
			"k8s.deployment.name": strAttrVal(name),
			"k8s.container.name":  strAttrVal(name),
		}

	case "eks":
		return map[string]AttrValue{
			"k8s.cluster.name":    strAttrVal("my-eks-cluster"),
			"k8s.namespace.name":  strAttrVal("default"),
			"k8s.pod.name":        strAttrVal(name + "-7d9f8b6c4-xzpqr"),
			"k8s.node.name":       strAttrVal("ip-10-0-1-100.ec2.internal"),
			"k8s.deployment.name": strAttrVal(name),
			"k8s.container.name":  strAttrVal(name),
			"cloud.provider":      strAttrVal("aws"),
			"cloud.platform":      strAttrVal("aws_eks"),
			"cloud.region":        strAttrVal("us-east-1"),
			"cloud.account.id":    strAttrVal("123456789012"),
		}

	case "gke":
		return map[string]AttrValue{
			"k8s.cluster.name":    strAttrVal("my-gke-cluster"),
			"k8s.namespace.name":  strAttrVal("default"),
			"k8s.pod.name":        strAttrVal(name + "-7d9f8b6c4-xzpqr"),
			"k8s.node.name":       strAttrVal("gke-my-cluster-default-pool-abc12345-abcd"),
			"k8s.deployment.name": strAttrVal(name),
			"k8s.container.name":  strAttrVal(name),
			"cloud.provider":      strAttrVal("gcp"),
			"cloud.platform":      strAttrVal("gcp_kubernetes_engine"),
			"cloud.region":        strAttrVal("us-central1"),
			"cloud.account.id":    strAttrVal("my-gcp-project"),
		}

	case "aks":
		return map[string]AttrValue{
			"k8s.cluster.name":    strAttrVal("my-aks-cluster"),
			"k8s.namespace.name":  strAttrVal("default"),
			"k8s.pod.name":        strAttrVal(name + "-7d9f8b6c4-xzpqr"),
			"k8s.node.name":       strAttrVal("aks-nodepool1-12345678-vmss000000"),
			"k8s.deployment.name": strAttrVal(name),
			"k8s.container.name":  strAttrVal(name),
			"cloud.provider":      strAttrVal("azure"),
			"cloud.platform":      strAttrVal("azure_aks"),
			"cloud.region":        strAttrVal("eastus"),
		}

	case "ecs":
		return map[string]AttrValue{
			"aws.ecs.cluster.arn":   strAttrVal("arn:aws:ecs:us-east-1:123456789012:cluster/my-cluster"),
			"aws.ecs.task.arn":      strAttrVal("arn:aws:ecs:us-east-1:123456789012:task/my-cluster/abcdef1234567890abcdef12"),
			"aws.ecs.task.family":   strAttrVal(name),
			"aws.ecs.container.name": strAttrVal(name),
			"aws.ecs.container.arn": strAttrVal("arn:aws:ecs:us-east-1:123456789012:container/my-cluster/abcdef1234567890abcdef12/a1b2c3d4-e5f6-7890-abcd-ef1234567890"),
			"cloud.provider":        strAttrVal("aws"),
			"cloud.platform":        strAttrVal("aws_ecs"),
			"cloud.region":          strAttrVal("us-east-1"),
			"cloud.account.id":      strAttrVal("123456789012"),
		}

	case "host":
		return map[string]AttrValue{
			"host.name":  strAttrVal("prod-server-01"),
			"host.id":    strAttrVal("i-0abcdef1234567890"),
			"host.type":  strAttrVal("m5.large"),
			"host.arch":  strAttrVal("amd64"),
			"os.type":    strAttrVal("linux"),
			"os.version": strAttrVal("5.15.0"),
		}

	case "docker":
		return map[string]AttrValue{
			"container.name":       strAttrVal(name),
			"container.id":         strAttrVal("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"),
			"container.image.name": strAttrVal(name),
			"container.image.tag":  strAttrVal("latest"),
			"host.name":            strAttrVal("docker-host-01"),
		}

	case "lambda":
		return map[string]AttrValue{
			"cloud.provider":  strAttrVal("aws"),
			"cloud.platform":  strAttrVal("aws_lambda"),
			"cloud.region":    strAttrVal("us-east-1"),
			"cloud.account.id": strAttrVal("123456789012"),
			"faas.name":       strAttrVal(name),
			"faas.version":    strAttrVal("$LATEST"),
			"faas.max_memory": intAttrVal(512),
		}

	case "cloudfoundry":
		return map[string]AttrValue{
			"cloudfoundry.app.id":      strAttrVal("abc12345-def6-7890-abcd-ef1234567890"),
			"cloudfoundry.app.name":    strAttrVal(name),
			"cloudfoundry.space.name":  strAttrVal("development"),
			"cloudfoundry.org.name":    strAttrVal("my-org"),
			"cloudfoundry.instance.id": strAttrVal("0"),
		}

	case "process":
		return map[string]AttrValue{
			"process.pid":             intAttrVal(12345),
			"process.executable.name": strAttrVal(name),
			"process.runtime.name":    strAttrVal("go"),
			"process.runtime.version": strAttrVal("1.24.0"),
			"host.name":               strAttrVal("localhost"),
		}

	case "openshift":
		return map[string]AttrValue{
			"k8s.cluster.name":    strAttrVal("my-ocp-cluster"),
			"k8s.namespace.name":  strAttrVal("default"),
			"k8s.pod.name":        strAttrVal(name + "-7d9f8b6c4-xzpqr"),
			"k8s.node.name":       strAttrVal("ocp-worker-1"),
			"k8s.deployment.name": strAttrVal(name),
			"k8s.container.name":  strAttrVal(name),
			"cloud.platform":      strAttrVal("openshift"),
		}

	case "containerd":
		return map[string]AttrValue{
			"container.runtime":    strAttrVal("containerd"),
			"container.name":       strAttrVal(name),
			"container.id":         strAttrVal("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"),
			"container.image.name": strAttrVal(name),
			"container.image.tag":  strAttrVal("latest"),
			"host.name":            strAttrVal("containerd-host-01"),
		}

	case "nomad":
		return map[string]AttrValue{
			"nomad.job.name":   strAttrVal(name),
			"nomad.task.name":  strAttrVal(name + "-task"),
			"nomad.namespace":  strAttrVal("default"),
			"nomad.datacenter": strAttrVal("dc1"),
			"nomad.alloc.id":   strAttrVal("abc12345-def6-7890-abcd-ef1234567890"),
		}

	case "azure-functions":
		return map[string]AttrValue{
			"cloud.provider":  strAttrVal("azure"),
			"cloud.platform":  strAttrVal("azure_functions"),
			"cloud.region":    strAttrVal("westeurope"),
			"cloud.account.id": strAttrVal("12345678-1234-1234-1234-123456789012"),
			"faas.name":       strAttrVal(name),
			"faas.version":    strAttrVal("1.0.0"),
		}

	case "gcp-functions":
		return map[string]AttrValue{
			"cloud.provider":  strAttrVal("gcp"),
			"cloud.platform":  strAttrVal("gcp_cloud_functions"),
			"cloud.region":    strAttrVal("europe-west1"),
			"cloud.account.id": strAttrVal("my-gcp-project"),
			"faas.name":       strAttrVal(name),
			"faas.version":    strAttrVal("1"),
		}

	case "azure-container-apps":
		return map[string]AttrValue{
			"cloud.provider":       strAttrVal("azure"),
			"cloud.platform":       strAttrVal("azure_container_apps"),
			"cloud.region":         strAttrVal("westeurope"),
			"cloud.account.id":     strAttrVal("12345678-1234-1234-1234-123456789012"),
			"container.name":       strAttrVal(name),
			"container.image.name": strAttrVal(name),
			"container.image.tag":  strAttrVal("latest"),
			"k8s.namespace.name":   strAttrVal(name + "-app"),
		}
	}

	return nil
}

func toOTLPAttributes(values map[string]AttrValue) []*commonpb.KeyValue {
	attrs := make([]*commonpb.KeyValue, 0, len(values))
	for k, v := range values {
		switch v.Type {
		case "bool":
			attrs = append(attrs, boolAttr(k, v.Bool))
		case "int":
			attrs = append(attrs, intAttr(k, v.Int))
		case "double":
			attrs = append(attrs, doubleAttr(k, v.Double))
		default:
			attrs = append(attrs, stringAttr(k, v.Str))
		}
	}
	return attrs
}

func newSpan(svc Service, traceID, spanID []byte, start, end time.Time, failed bool) *tracepb.Span {
	name, tmplAttrs := templateInfo(svc, failed)
	span := &tracepb.Span{
		TraceId:           traceID,
		SpanId:            spanID,
		Name:              name,
		Kind:              mapSpanKind(svc.SpanKind),
		StartTimeUnixNano: uint64(start.UnixNano()),
		EndTimeUnixNano:   uint64(end.UnixNano()),
	}
	applyFailure(span, failed)
	tmplAttrs = applySpanAttrOverrides(tmplAttrs, svc.SpanAttrs)
	span.Attributes = append(span.Attributes, tmplAttrs...)
	return span
}

func applyFailure(span *tracepb.Span, failed bool) {
	if !failed {
		span.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}
		span.Attributes = append(span.Attributes, stringAttr("otgen.outcome", "success"))
		return
	}
	span.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "simulated failure"}
	span.Attributes = append(span.Attributes, stringAttr("otgen.outcome", "failure"))
}

// ── template span attributes ──────────────────────────────────────────────────

var (
	httpMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH"}
	httpPaths   = []string{"/api/users", "/api/orders", "/api/products", "/api/auth", "/health", "/api/events", "/api/payments"}
	httpHosts   = []string{"api.example.com", "payment.internal", "user-service.internal", "order-service.internal"}
	dbSystems   = []string{"postgresql", "mysql", "mongodb", "redis", "cassandra"}
	dbNamespaces  = []string{"orders", "users", "analytics", "inventory", "sessions"}
	dbCollections = []string{"users", "orders", "products", "sessions", "events", "payments"}
	dbOps         = []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
	msgSystems    = []string{"kafka", "rabbitmq", "aws_sqs"}
	msgTopics     = []string{"orders", "events", "notifications", "alerts", "payments"}
	msgOps        = []string{"publish", "receive", "process", "settle"}
	grpcSvcs      = []string{"UserService", "OrderService", "PaymentService", "InventoryService", "NotificationService"}
	grpcMethods   = map[string][]string{
		"UserService":         {"GetUser", "CreateUser", "UpdateUser", "ListUsers"},
		"OrderService":        {"CreateOrder", "GetOrder", "ListOrders", "CancelOrder"},
		"PaymentService":      {"ProcessPayment", "RefundPayment", "GetPaymentStatus"},
		"InventoryService":    {"CheckStock", "ReserveStock", "ReleaseStock"},
		"NotificationService": {"SendNotification", "GetNotificationStatus"},
	}
)

// templateDefaults returns the editable/overridable attribute defaults for a
// given template. These values are used to seed the span-attrs editor and as
// fallback when SpanAttrs is empty.
func templateDefaults(template string) map[string]AttrValue {
	switch template {
	case "http-server":
		return map[string]AttrValue{
			"server.address":           strAttrVal("my-service.internal"),
			"url.scheme":               strAttrVal("https"),
			"network.protocol.version": strAttrVal("1.1"),
		}
	case "http-client":
		return map[string]AttrValue{
			"server.address":           strAttrVal("api.example.com"),
			"server.port":              intAttrVal(443),
			"network.protocol.version": strAttrVal("1.1"),
		}
	case "db":
		return map[string]AttrValue{
			"db.system.name": strAttrVal("postgresql"),
			"db.namespace":   strAttrVal("mydb"),
			"server.address": strAttrVal("db.internal"),
			"server.port":    intAttrVal(5432),
		}
	case "messaging":
		return map[string]AttrValue{
			"messaging.system":           strAttrVal("kafka"),
			"messaging.destination.name": strAttrVal("my-topic"),
		}
	case "grpc":
		return map[string]AttrValue{
			"rpc.service": strAttrVal("MyService"),
			"rpc.method":  strAttrVal("MyMethod"),
		}
	}
	return nil
}

// templateInfo returns the span name and extra semantic-convention attributes
// for svc.Template, or a generic name and nil attrs when no template is set.
// Values in svc.SpanAttrs override randomly selected template defaults.
func templateInfo(svc Service, failed bool) (string, []*commonpb.KeyValue) {
	rnd := func(items []string) string { return items[mathrand.IntN(len(items))] }

	switch svc.Template {
	case "http-server":
		method := rnd(httpMethods)
		path := rnd(httpPaths)
		status := int64(200)
		if failed {
			status = 500
		}
		return method + " " + path, []*commonpb.KeyValue{
			stringAttr("http.request.method", method),
			stringAttr("url.path", path),
			stringAttr("url.scheme", "https"),
			intAttr("http.response.status_code", status),
			stringAttr("server.address", svc.Name+".service"),
			stringAttr("network.protocol.version", "1.1"),
		}

	case "http-client":
		method := rnd(httpMethods[:2]) // GET or POST
		host := rnd(httpHosts)
		path := rnd(httpPaths)
		status := int64(200)
		if failed {
			status = 502
		}
		return method + " " + host, []*commonpb.KeyValue{
			stringAttr("http.request.method", method),
			stringAttr("server.address", host),
			intAttr("server.port", 443),
			stringAttr("url.full", "https://"+host+path),
			intAttr("http.response.status_code", status),
			stringAttr("network.protocol.version", "1.1"),
		}

	case "db":
		system := rnd(dbSystems)
		ns := rnd(dbNamespaces)
		col := rnd(dbCollections)
		op := rnd(dbOps)
		port := int64(5432)
		switch system {
		case "mysql":
			port = 3306
		case "mongodb":
			port = 27017
		case "redis":
			port = 6379
		case "cassandra":
			port = 9042
		}
		return op + " " + col, []*commonpb.KeyValue{
			stringAttr("db.system.name", system),
			stringAttr("db.namespace", ns),
			stringAttr("db.operation.name", op),
			stringAttr("db.collection.name", col),
			stringAttr("server.address", "db.internal"),
			intAttr("server.port", port),
		}

	case "messaging":
		system := rnd(msgSystems)
		topic := rnd(msgTopics)
		op := rnd(msgOps)
		return topic + " " + op, []*commonpb.KeyValue{
			stringAttr("messaging.system", system),
			stringAttr("messaging.destination.name", topic),
			stringAttr("messaging.operation.name", op),
			stringAttr("messaging.message.id", randomHex(8)),
			stringAttr("messaging.client_id", svc.Name+"-client"),
		}

	case "grpc":
		grpcSvc := rnd(grpcSvcs)
		method := rnd(grpcMethods[grpcSvc])
		statusCode := int64(0) // OK
		if failed {
			statusCode = 13 // INTERNAL
		}
		return "/" + grpcSvc + "/" + method, []*commonpb.KeyValue{
			stringAttr("rpc.system", "grpc"),
			stringAttr("rpc.service", grpcSvc),
			stringAttr("rpc.method", method),
			intAttr("rpc.grpc.status_code", statusCode),
		}
	}

	// no template → generic name
	return svc.Name + ".request", nil
}

// applySpanAttrOverrides merges user-supplied SpanAttrs on top of the
// template-generated attribute list. Known keys are replaced; new keys appended.
func applySpanAttrOverrides(attrs []*commonpb.KeyValue, overrides map[string]AttrValue) []*commonpb.KeyValue {
	if len(overrides) == 0 {
		return attrs
	}
	// index existing attrs by key
	idx := make(map[string]int, len(attrs))
	for i, a := range attrs {
		idx[a.Key] = i
	}
	for _, ov := range toOTLPAttributes(overrides) {
		if i, exists := idx[ov.Key]; exists {
			attrs[i] = ov
		} else {
			attrs = append(attrs, ov)
		}
	}
	return attrs
}

func mapSpanKind(value string) tracepb.Span_SpanKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "server":
		return tracepb.Span_SPAN_KIND_SERVER
	case "client":
		return tracepb.Span_SPAN_KIND_CLIENT
	case "producer":
		return tracepb.Span_SPAN_KIND_PRODUCER
	case "consumer":
		return tracepb.Span_SPAN_KIND_CONSUMER
	default:
		return tracepb.Span_SPAN_KIND_INTERNAL
	}
}

// randomRootDuration returns a random root span duration between 50 and 500 ms.
func randomRootDuration() time.Duration {
	return time.Duration(50+mathrand.IntN(451)) * time.Millisecond
}

// randomChildDuration returns a random child span duration between 5 and 80 ms.
func randomChildDuration() time.Duration {
	return time.Duration(5+mathrand.IntN(76)) * time.Millisecond
}

// childSpanOps are the operation names cycled through for child spans.
var childSpanOps = []string{
	"db.query",
	"cache.lookup",
	"http.request",
	"queue.publish",
	"grpc.call",
	"db.transaction",
	"auth.validate",
	"storage.read",
	"storage.write",
	"event.emit",
}

func newChildSpan(svcName string, traceID, spanID, parentID []byte, start, end time.Time, idx int, failed bool) *tracepb.Span {
	op := childSpanOps[idx%len(childSpanOps)]
	status := &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}
	if failed {
		status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "simulated failure"}
	}
	return &tracepb.Span{
		TraceId:           traceID,
		SpanId:            spanID,
		ParentSpanId:      parentID,
		Name:              svcName + "." + op,
		Kind:              tracepb.Span_SPAN_KIND_CLIENT,
		StartTimeUnixNano: uint64(start.UnixNano()),
		EndTimeUnixNano:   uint64(end.UnixNano()),
		Status:            status,
	}
}

func mustDecodeHex(value string, byteLen int) []byte {
	b, err := hex.DecodeString(value)
	if err != nil || len(b) != byteLen {
		return make([]byte, byteLen)
	}
	return b
}

func randomHex(byteLen int) string {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", byteLen*2)
	}
	return hex.EncodeToString(buf)
}

func stringAttr(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}

func intAttr(key string, value int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: value}}}
}

func boolAttr(key string, value bool) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: value}}}
}

func doubleAttr(key string, value float64) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: value}}}
}

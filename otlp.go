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
		spanID := mustDecodeHex(randomHex(8), 8)
		failed := mathrand.IntN(100) < svc.FailureRate
		dur := randomDuration()
		end := now.Add(dur)
		span := newSpan(svc, traceID, spanID, now, end, failed)
		return proto.Marshal(&collectortracepb.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{{
				Resource:   resource,
				ScopeSpans: []*tracepb.ScopeSpans{{Scope: scope, Spans: []*tracepb.Span{span}}},
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

// svcAttributes builds the resource attribute list for a service, always
// including service.name (which wins over any caller-supplied value).
func svcAttributes(svc Service) []*commonpb.KeyValue {
	merged := make(map[string]AttrValue, len(svc.Attributes)+1)
	for k, v := range svc.Attributes {
		merged[k] = v
	}
	merged["service.name"] = strAttrVal(svc.Name) // always wins
	return toOTLPAttributes(merged)
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
	span := &tracepb.Span{
		TraceId:           traceID,
		SpanId:            spanID,
		Name:              svc.Name + ".request",
		Kind:              mapSpanKind(svc.SpanKind),
		StartTimeUnixNano: uint64(start.UnixNano()),
		EndTimeUnixNano:   uint64(end.UnixNano()),
	}
	applyFailure(span, failed)
	return span
}

func applyFailure(span *tracepb.Span, failed bool) {
	if !failed {
		span.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}
		span.Attributes = append(span.Attributes, stringAttr("otgen.outcome", "success"))
		return
	}
	span.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "simulated failure"}
	span.Attributes = append(span.Attributes,
		stringAttr("otgen.outcome", "failure"),
		stringAttr("error.type", "http_error"),
		intAttr("http.response.status_code", 500),
	)
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

// randomDuration returns a random span duration between 20 and 200 ms.
func randomDuration() time.Duration {
	return time.Duration(20+mathrand.IntN(181)) * time.Millisecond
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

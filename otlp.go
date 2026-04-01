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

func buildPayload(cfg AppConfig, kind signalKind) ([]byte, error) {
	now := time.Now()
	resource := &resourcepb.Resource{Attributes: toOTLPAttributes(cfg.ResourceAttributes)}
	scope := &commonpb.InstrumentationScope{Name: "otlpforge", Version: "1.0.0"}

	switch kind {
	case signalSpans:
		traceID := mustDecodeHex(randomHex(16), 16)
		rootSpanID := mustDecodeHex(randomHex(8), 8)
		failed := mathrand.IntN(100) < cfg.SpanFailureRate
		rootDuration := randomDuration(cfg.SpanMinDurationMs, cfg.SpanMaxDurationMs)
		rootStart := now
		rootEnd := now.Add(rootDuration)
		spans := []*tracepb.Span{newRootSpan(cfg, traceID, rootSpanID, rootStart, rootEnd, failed)}
		spans = append(spans, newChildSpans(cfg, traceID, rootSpanID, rootStart, rootEnd, failed)...)

		return proto.Marshal(&collectortracepb.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{{
				Resource: resource,
				ScopeSpans: []*tracepb.ScopeSpans{{
					Scope: scope,
					Spans: spans,
				}},
			}},
		})
	case signalMetrics:
		return proto.Marshal(&collectormetricspb.ExportMetricsServiceRequest{
			ResourceMetrics: []*metricspb.ResourceMetrics{{
				Resource: resource,
				ScopeMetrics: []*metricspb.ScopeMetrics{{
					Scope: scope,
					Metrics: []*metricspb.Metric{{
						Name: cfg.MetricName,
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
						Body:           &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: cfg.LogMessage}},
					}},
				}},
			}},
		})
	default:
		return nil, fmt.Errorf("unsupported signal type: %s", kind)
	}
}

func toOTLPAttributes(values map[string]string) []*commonpb.KeyValue {
	attrs := make([]*commonpb.KeyValue, 0, len(values))
	for k, v := range values {
		attrs = append(attrs, &commonpb.KeyValue{
			Key:   k,
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}},
		})
	}
	return attrs
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

func formatTokenHeader(headerName, token string) string {
	if strings.EqualFold(headerName, "Authorization") && !strings.HasPrefix(strings.ToLower(token), "api-token ") {
		return "Api-Token " + token
	}
	return token
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

func newRootSpan(cfg AppConfig, traceID, spanID []byte, start, end time.Time, failed bool) *tracepb.Span {
	span := &tracepb.Span{
		TraceId:           traceID,
		SpanId:            spanID,
		Name:              cfg.SpanName,
		Kind:              mapSpanKind(cfg.SpanKind),
		StartTimeUnixNano: uint64(start.UnixNano()),
		EndTimeUnixNano:   uint64(end.UnixNano()),
	}
	applyFailure(span, cfg, failed)
	return span
}

func newChildSpans(cfg AppConfig, traceID, parentSpanID []byte, rootStart, rootEnd time.Time, failed bool) []*tracepb.Span {
	if cfg.SpanChildCount <= 0 {
		return nil
	}

	children := make([]*tracepb.Span, 0, cfg.SpanChildCount)
	total := rootEnd.Sub(rootStart)
	step := total / time.Duration(cfg.SpanChildCount+1)
	if step <= 0 {
		step = time.Millisecond
	}

	for i := 0; i < cfg.SpanChildCount; i++ {
		start := rootStart.Add(time.Duration(i+1)*step - step/2)
		if start.Before(rootStart) {
			start = rootStart
		}
		duration := randomDuration(cfg.SpanMinDurationMs/2, cfg.SpanMaxDurationMs/2)
		end := start.Add(duration)
		if end.After(rootEnd) {
			end = rootEnd
		}
		child := &tracepb.Span{
			TraceId:           traceID,
			SpanId:            mustDecodeHex(randomHex(8), 8),
			ParentSpanId:      parentSpanID,
			Name:              fmt.Sprintf("%s.child.%d", cfg.SpanName, i+1),
			Kind:              tracepb.Span_SPAN_KIND_INTERNAL,
			StartTimeUnixNano: uint64(start.UnixNano()),
			EndTimeUnixNano:   uint64(end.UnixNano()),
		}
		applyFailure(child, cfg, failed && i == cfg.SpanChildCount-1)
		children = append(children, child)
	}
	return children
}

func applyFailure(span *tracepb.Span, cfg AppConfig, failed bool) {
	if !failed {
		span.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}
		span.Attributes = append(span.Attributes, stringAttr("otlpforge.outcome", "success"))
		return
	}

	span.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: cfg.SpanFailureMessage}
	span.Attributes = append(span.Attributes,
		stringAttr("otlpforge.outcome", "failure"),
		stringAttr("otlpforge.failure_mode", cfg.SpanFailureMode),
	)

	switch strings.ToLower(strings.TrimSpace(cfg.SpanFailureMode)) {
	case "timeout":
		span.Attributes = append(span.Attributes,
			stringAttr("error.type", "timeout"),
			boolAttr("timeout", true),
		)
	case "backend":
		span.Attributes = append(span.Attributes,
			stringAttr("error.type", "backend_error"),
		)
	default:
		span.Attributes = append(span.Attributes,
			stringAttr("error.type", "http_error"),
			intAttr("http.response.status_code", int64(cfg.SpanFailureCode)),
		)
	}
}

func randomDuration(minMs, maxMs int) time.Duration {
	if minMs <= 0 {
		minMs = 1
	}
	if maxMs < minMs {
		maxMs = minMs
	}
	if minMs == maxMs {
		return time.Duration(minMs) * time.Millisecond
	}
	return time.Duration(minMs+mathrand.IntN(maxMs-minMs+1)) * time.Millisecond
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

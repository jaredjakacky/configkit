package otel_test

import (
	"context"
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
	otel "github.com/jaredjakacky/configkit/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestNewObserverUsesNoopProvidersWhenNil(t *testing.T) {
	observer, err := otel.NewObserver(nil, nil)
	if err != nil {
		t.Fatalf("new observer: %v", err)
	}
	if observer == nil {
		t.Fatal("observer = nil, want observer")
	}

	observer(nil, configkit.Event{Kind: configkit.EventKindLoadStarted})
}

func TestObserverHandlesPublicEventKinds(t *testing.T) {
	observer, err := otel.NewObserver(
		metricnoop.NewMeterProvider().Meter("configkit-test"),
		tracenoop.NewTracerProvider().Tracer("configkit-test"),
		otel.WithSourceName(),
		nil,
	)
	if err != nil {
		t.Fatalf("new observer: %v", err)
	}

	event := otelTestEvent(configkit.EventKindLoadSucceeded)
	observer(nil, configkit.Event{Kind: configkit.EventKindLoadStarted, Source: event.Source})
	observer(context.Background(), event)
	observer(context.Background(), otelTestEvent(configkit.EventKindLoadFailed))
	observer(context.Background(), configkit.Event{
		Kind:       configkit.EventKindSnapshotApplied,
		Source:     event.Source,
		Snapshot:   event.Snapshot,
		Apply:      &configkit.ApplyResult{Published: true, Changed: true},
		OccurredAt: event.OccurredAt,
	})
}

func TestObserverOmitsRawConfigFromEventContract(t *testing.T) {
	observer, err := otel.NewObserver(nil, nil)
	if err != nil {
		t.Fatalf("new observer: %v", err)
	}

	observer(context.Background(), configkit.Event{
		Kind: configkit.EventKindSnapshotApplied,
		Snapshot: &configkit.SnapshotMetadata{
			Source:   configkit.SourceMetadata{Name: "source", Kind: "memory"},
			Revision: "rev-1",
			Checksum: "sum-1",
			LoadedAt: time.Now(),
		},
		Apply: &configkit.ApplyResult{Published: true, Changed: true},
	})
}

func TestObserverRecordsLoadAndApplySpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown tracer provider: %v", err)
		}
	}()
	observer, err := otel.NewObserver(metricnoop.NewMeterProvider().Meter("configkit-test"), tracerProvider.Tracer("configkit-test"), otel.WithSourceName())
	if err != nil {
		t.Fatalf("new observer: %v", err)
	}

	loadEvent := otelTestEvent(configkit.EventKindLoadFailed)
	applyEvent := configkit.Event{
		Kind:       configkit.EventKindSnapshotApplied,
		Source:     loadEvent.Source,
		Snapshot:   loadEvent.Snapshot,
		Apply:      &configkit.ApplyResult{Published: true, Changed: true},
		OccurredAt: loadEvent.OccurredAt,
	}
	observer(context.Background(), loadEvent)
	observer(context.Background(), applyEvent)

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("ended span count = %d, want 2", len(spans))
	}
	loadSpan := findSpan(t, spans, "configkit.load")
	if loadSpan.Status().Code != codes.Error {
		t.Fatalf("load span status = %v, want error", loadSpan.Status().Code)
	}
	if got := loadSpan.StartTime(); !got.Equal(loadEvent.Attempt.StartedAt) {
		t.Fatalf("load span start = %v, want %v", got, loadEvent.Attempt.StartedAt)
	}
	if got := loadSpan.EndTime(); !got.Equal(loadEvent.Attempt.EndedAt) {
		t.Fatalf("load span end = %v, want %v", got, loadEvent.Attempt.EndedAt)
	}
	assertOtelAttrs(t, loadSpan.Attributes(), map[string]string{
		"configkit.event":          string(configkit.EventKindLoadFailed),
		"configkit.attempt.kind":   string(configkit.AttemptKindReload),
		"configkit.attempt.status": string(configkit.AttemptStatusFailed),
		"configkit.attempt.stage":  string(configkit.AttemptStageDecode),
		"configkit.source.kind":    "memory",
		"configkit.source.name":    "config-source",
	})
	assertOtelAttrsOmit(t, loadSpan.Attributes(), "configkit.revision", "configkit.checksum", "configkit.redacted")

	applySpan := findSpan(t, spans, "configkit.apply")
	if applySpan.Status().Code != codes.Ok {
		t.Fatalf("apply span status = %v, want ok", applySpan.Status().Code)
	}
	if got := applySpan.StartTime(); !got.Equal(applyEvent.OccurredAt) {
		t.Fatalf("apply span start = %v, want %v", got, applyEvent.OccurredAt)
	}
	if got := applySpan.EndTime(); !got.Equal(applyEvent.OccurredAt) {
		t.Fatalf("apply span end = %v, want %v", got, applyEvent.OccurredAt)
	}
}

func TestObserverRecordsMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() {
		if err := meterProvider.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown meter provider: %v", err)
		}
	}()
	observer, err := otel.NewObserver(meterProvider.Meter("configkit-test"), tracenoop.NewTracerProvider().Tracer("configkit-test"))
	if err != nil {
		t.Fatalf("new observer: %v", err)
	}

	loadEvent := otelTestEvent(configkit.EventKindLoadFailed)
	observer(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted, Source: loadEvent.Source})
	observer(context.Background(), loadEvent)
	observer(context.Background(), configkit.Event{
		Kind:       configkit.EventKindSnapshotApplied,
		Source:     loadEvent.Source,
		Apply:      &configkit.ApplyResult{Published: true, Changed: true},
		OccurredAt: loadEvent.OccurredAt,
	})

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if got := int64MetricValue(t, metrics, "configkit.load.started"); got != 1 {
		t.Fatalf("load started metric = %d, want 1", got)
	}
	if got := int64MetricValue(t, metrics, "configkit.load.completed"); got != 1 {
		t.Fatalf("load completed metric = %d, want 1", got)
	}
	if got := int64MetricValue(t, metrics, "configkit.load.failed"); got != 1 {
		t.Fatalf("load failed metric = %d, want 1", got)
	}
	if got := int64MetricValue(t, metrics, "configkit.apply.published"); got != 1 {
		t.Fatalf("apply published metric = %d, want 1", got)
	}
	if got := int64MetricValue(t, metrics, "configkit.apply.changed"); got != 1 {
		t.Fatalf("apply changed metric = %d, want 1", got)
	}
	if got := histogramCount(t, metrics, "configkit.load.duration"); got != 1 {
		t.Fatalf("load duration count = %d, want 1", got)
	}
}

func otelTestEvent(kind configkit.EventKind) configkit.Event {
	startedAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(time.Second)
	status := configkit.AttemptStatusSucceeded
	if kind == configkit.EventKindLoadFailed {
		status = configkit.AttemptStatusFailed
	}

	return configkit.Event{
		Kind:        kind,
		AttemptID:   1,
		AttemptKind: configkit.AttemptKindReload,
		Source:      configkit.SourceMetadata{Name: "config-source", Kind: "memory"},
		Revision:    "rev-1",
		Attempt: &configkit.AttemptRecord{
			ID:        1,
			Kind:      configkit.AttemptKindReload,
			Status:    status,
			Stage:     configkit.AttemptStageDecode,
			Source:    configkit.SourceMetadata{Name: "config-source", Kind: "memory"},
			Revision:  "rev-1",
			Checksum:  "sum-1",
			StartedAt: startedAt,
			EndedAt:   endedAt,
			Error:     "decode failed",
		},
		Snapshot: &configkit.SnapshotMetadata{
			Source:   configkit.SourceMetadata{Name: "config-source", Kind: "memory"},
			Revision: "rev-1",
			Checksum: "sum-1",
			LoadedAt: endedAt,
		},
		OccurredAt: endedAt,
	}
}

func findSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()

	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %q not found", name)
	return nil
}

func assertOtelAttrs(t *testing.T, attrs []attribute.KeyValue, want map[string]string) {
	t.Helper()

	got := otelAttrsByKey(attrs)
	for key, wantValue := range want {
		value, ok := got[key]
		if !ok {
			t.Fatalf("attribute %q missing", key)
		}
		if value.AsString() != wantValue {
			t.Fatalf("attribute %q = %q, want %q", key, value.AsString(), wantValue)
		}
	}
}

func assertOtelAttrsOmit(t *testing.T, attrs []attribute.KeyValue, keys ...string) {
	t.Helper()

	got := otelAttrsByKey(attrs)
	for _, key := range keys {
		if _, ok := got[key]; ok {
			t.Fatalf("attribute %q present, want omitted", key)
		}
	}
}

func otelAttrsByKey(attrs []attribute.KeyValue) map[string]attribute.Value {
	byKey := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		byKey[string(attr.Key)] = attr.Value
	}
	return byKey
}

func int64MetricValue(t *testing.T, metrics metricdata.ResourceMetrics, name string) int64 {
	t.Helper()

	metric := findMetric(t, metrics, name)
	sum, ok := metric.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("metric %q data type = %T, want int64 sum", name, metric.Data)
	}
	var total int64
	for _, point := range sum.DataPoints {
		total += point.Value
	}
	return total
}

func histogramCount(t *testing.T, metrics metricdata.ResourceMetrics, name string) uint64 {
	t.Helper()

	metric := findMetric(t, metrics, name)
	histogram, ok := metric.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("metric %q data type = %T, want float64 histogram", name, metric.Data)
	}
	var total uint64
	for _, point := range histogram.DataPoints {
		total += point.Count
	}
	return total
}

func findMetric(t *testing.T, metrics metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()

	for _, scope := range metrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == name {
				return metric
			}
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}

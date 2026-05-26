package otel

import (
	"context"
	"errors"
	"fmt"
	"time"

	configkit "github.com/jaredjakacky/configkit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const instrumentationName = "github.com/jaredjakacky/configkit/otel"

// Option configures an OpenTelemetry observer.
type Option func(*options)

type options struct {
	sourceName bool
}

// WithSourceName includes SourceMetadata.Name as a metric attribute.
//
// Source names can have higher cardinality than source kinds. The observer
// excludes source names by default so Kubernetes-style deployments get useful
// metrics without accidental label cardinality growth.
func WithSourceName() Option {
	return func(options *options) {
		options.sourceName = true
	}
}

// NewObserver creates a Configkit observer that records OpenTelemetry metrics
// and traces.
//
// If meter or tracer is nil, NewObserver uses the corresponding OpenTelemetry
// no-op provider. This keeps local and incremental wiring simple while allowing
// production services to pass their configured Meter and Tracer.
//
// The observer records load, failure, duration, publish, and changed counters.
// It also creates spans for manager-owned load and apply operations.
// It does not record revision, checksum, raw config data, redacted config data,
// or typed config values.
func NewObserver(meter metric.Meter, tracer trace.Tracer, opts ...Option) (configkit.Observer, error) {
	var options options
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	if meter == nil {
		meter = metricnoop.NewMeterProvider().Meter(instrumentationName)
	}
	if tracer == nil {
		tracer = tracenoop.NewTracerProvider().Tracer(instrumentationName)
	}

	observer, err := newObserver(meter, tracer, options)
	if err != nil {
		return nil, err
	}

	return observer.Observe, nil
}

type observer struct {
	options options
	tracer  trace.Tracer

	loadStarted    metric.Int64Counter
	loadCompleted  metric.Int64Counter
	loadFailed     metric.Int64Counter
	loadDuration   metric.Float64Histogram
	applyPublished metric.Int64Counter
	applyChanged   metric.Int64Counter
}

func newObserver(meter metric.Meter, tracer trace.Tracer, options options) (*observer, error) {
	loadStarted, err := meter.Int64Counter(
		"configkit.load.started",
		metric.WithDescription("Number of config load attempts started."),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create configkit load started counter: %w", err)
	}

	loadCompleted, err := meter.Int64Counter(
		"configkit.load.completed",
		metric.WithDescription("Number of config load attempts completed."),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create configkit load completed counter: %w", err)
	}

	loadFailed, err := meter.Int64Counter(
		"configkit.load.failed",
		metric.WithDescription("Number of failed config load attempts."),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create configkit load failed counter: %w", err)
	}

	loadDuration, err := meter.Float64Histogram(
		"configkit.load.duration",
		metric.WithDescription("Duration of completed config load attempts."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create configkit load duration histogram: %w", err)
	}

	applyPublished, err := meter.Int64Counter(
		"configkit.apply.published",
		metric.WithDescription("Number of successful snapshots published by a config manager."),
		metric.WithUnit("{snapshot}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create configkit apply published counter: %w", err)
	}

	applyChanged, err := meter.Int64Counter(
		"configkit.apply.changed",
		metric.WithDescription("Number of published snapshots whose checksum changed."),
		metric.WithUnit("{snapshot}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create configkit apply changed counter: %w", err)
	}

	return &observer{
		options:        options,
		tracer:         tracer,
		loadStarted:    loadStarted,
		loadCompleted:  loadCompleted,
		loadFailed:     loadFailed,
		loadDuration:   loadDuration,
		applyPublished: applyPublished,
		applyChanged:   applyChanged,
	}, nil
}

// Observe records metrics for one Configkit event.
func (o *observer) Observe(ctx context.Context, event configkit.Event) {
	if o == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	switch event.Kind {
	case configkit.EventKindLoadStarted:
		o.loadStarted.Add(ctx, 1, metric.WithAttributes(o.attrs(event)...))
	case configkit.EventKindLoadSucceeded, configkit.EventKindLoadFailed:
		attrs := o.attrs(event)
		o.loadCompleted.Add(ctx, 1, metric.WithAttributes(attrs...))

		if event.Kind == configkit.EventKindLoadFailed {
			o.loadFailed.Add(ctx, 1, metric.WithAttributes(attrs...))
		}

		if duration, ok := attemptDuration(event.Attempt); ok {
			o.loadDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
		}
		o.finishLoadSpan(ctx, event, attrs)
	case configkit.EventKindSnapshotApplied:
		attrs := o.attrs(event)
		o.applyPublished.Add(ctx, 1, metric.WithAttributes(attrs...))
		if event.Apply != nil && event.Apply.Changed {
			o.applyChanged.Add(ctx, 1, metric.WithAttributes(attrs...))
		}
		o.recordApplySpan(ctx, event, attrs)
	}
}

func (o *observer) finishLoadSpan(ctx context.Context, event configkit.Event, attrs []attribute.KeyValue) {
	_, span := o.tracer.Start(ctx, "configkit.load", trace.WithAttributes(attrs...), trace.WithTimestamp(attemptStartTime(event)))

	span.SetAttributes(attrs...)
	span.AddEvent(string(event.Kind), trace.WithAttributes(attrs...), trace.WithTimestamp(eventTime(event)))
	if event.Kind == configkit.EventKindLoadFailed {
		err := eventError(event)
		if err != nil {
			span.RecordError(err, trace.WithTimestamp(eventTime(event)))
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Error, "config load failed")
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}

	span.End(trace.WithTimestamp(attemptEndTime(event)))
}

func (o *observer) recordApplySpan(ctx context.Context, event configkit.Event, attrs []attribute.KeyValue) {
	_, span := o.tracer.Start(ctx, "configkit.apply", trace.WithAttributes(attrs...), trace.WithTimestamp(eventTime(event)))
	defer span.End(trace.WithTimestamp(eventTime(event)))

	span.SetStatus(codes.Ok, "")
	span.AddEvent(string(event.Kind), trace.WithAttributes(attrs...), trace.WithTimestamp(eventTime(event)))
}

func (o *observer) attrs(event configkit.Event) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("configkit.event", string(event.Kind)),
	}

	if kind := attemptKind(event); kind != "" {
		attrs = append(attrs, attribute.String("configkit.attempt.kind", string(kind)))
	}
	if status := attemptStatus(event); status != "" {
		attrs = append(attrs, attribute.String("configkit.attempt.status", string(status)))
	}
	if stage := attemptStage(event); stage != "" {
		attrs = append(attrs, attribute.String("configkit.attempt.stage", string(stage)))
	}
	if source := sourceMetadata(event); source.Kind != "" {
		attrs = append(attrs, attribute.String("configkit.source.kind", source.Kind))
	}
	if o.options.sourceName {
		if source := sourceMetadata(event); source.Name != "" {
			attrs = append(attrs, attribute.String("configkit.source.name", source.Name))
		}
	}
	if event.Apply != nil {
		attrs = append(attrs, attribute.Bool("configkit.apply.changed", event.Apply.Changed))
	}

	return attrs
}

func attemptKind(event configkit.Event) configkit.AttemptKind {
	if event.AttemptKind != "" {
		return event.AttemptKind
	}
	if event.Attempt != nil {
		return event.Attempt.Kind
	}
	return ""
}

func attemptStatus(event configkit.Event) configkit.AttemptStatus {
	if event.Attempt != nil && event.Attempt.Status != "" {
		return event.Attempt.Status
	}

	switch event.Kind {
	case configkit.EventKindLoadSucceeded:
		return configkit.AttemptStatusSucceeded
	case configkit.EventKindLoadFailed:
		return configkit.AttemptStatusFailed
	default:
		return ""
	}
}

func attemptStage(event configkit.Event) configkit.AttemptStage {
	if event.Attempt == nil {
		return ""
	}
	return event.Attempt.Stage
}

func sourceMetadata(event configkit.Event) configkit.SourceMetadata {
	if event.Source != (configkit.SourceMetadata{}) {
		return event.Source
	}
	if event.Attempt != nil && event.Attempt.Source != (configkit.SourceMetadata{}) {
		return event.Attempt.Source
	}
	if event.Snapshot != nil {
		return event.Snapshot.Source
	}
	return configkit.SourceMetadata{}
}

func attemptDuration(attempt *configkit.AttemptRecord) (time.Duration, bool) {
	if attempt == nil || attempt.StartedAt.IsZero() || attempt.EndedAt.IsZero() {
		return 0, false
	}

	duration := attempt.EndedAt.Sub(attempt.StartedAt)
	if duration < 0 {
		return 0, false
	}

	return duration, true
}

func attemptStartTime(event configkit.Event) time.Time {
	if event.Attempt != nil && !event.Attempt.StartedAt.IsZero() {
		return event.Attempt.StartedAt
	}
	return eventTime(event)
}

func attemptEndTime(event configkit.Event) time.Time {
	if event.Attempt != nil && !event.Attempt.EndedAt.IsZero() {
		return event.Attempt.EndedAt
	}
	return eventTime(event)
}

func eventTime(event configkit.Event) time.Time {
	if !event.OccurredAt.IsZero() {
		return event.OccurredAt
	}
	return time.Now()
}

func eventError(event configkit.Event) error {
	if event.Attempt == nil || event.Attempt.Error == "" {
		return nil
	}
	return errors.New(event.Attempt.Error)
}

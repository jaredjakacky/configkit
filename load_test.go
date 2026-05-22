package configkit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	configkit "github.com/jaredjakacky/configkit"
)

func TestLoadSucceeds(t *testing.T) {
	result, err := configkit.Load(context.Background(), configkit.AttemptKindInitialLoad, configkit.SourceData{
		Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
		Metadata: configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		Revision: "rev-1",
	}, testPipeline())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if result.Snapshot == nil {
		t.Fatal("snapshot = nil, want snapshot")
	}
	if result.Attempt.Status != configkit.AttemptStatusSucceeded {
		t.Fatalf("attempt status = %q, want %q", result.Attempt.Status, configkit.AttemptStatusSucceeded)
	}
	if result.Attempt.Kind != configkit.AttemptKindInitialLoad {
		t.Fatalf("attempt kind = %q, want %q", result.Attempt.Kind, configkit.AttemptKindInitialLoad)
	}
	if result.Attempt.Source.Name != "memory" {
		t.Fatalf("attempt source name = %q, want memory", result.Attempt.Source.Name)
	}
	if result.Attempt.Revision != "rev-1" {
		t.Fatalf("attempt revision = %q, want rev-1", result.Attempt.Revision)
	}
	if result.Attempt.Checksum == "" {
		t.Fatal("attempt checksum = empty, want checksum")
	}
	if result.Attempt.StartedAt.IsZero() || result.Attempt.EndedAt.IsZero() {
		t.Fatalf("attempt times = %v/%v, want non-zero", result.Attempt.StartedAt, result.Attempt.EndedAt)
	}

	metadata := result.Snapshot.Metadata()
	if metadata.Revision != "rev-1" {
		t.Fatalf("snapshot revision = %q, want rev-1", metadata.Revision)
	}
	if metadata.Checksum != result.Attempt.Checksum {
		t.Fatalf("snapshot checksum = %q, want attempt checksum %q", metadata.Checksum, result.Attempt.Checksum)
	}
	if got := result.Snapshot.Value(); got.Name != "api" || !got.Enabled || got.Port != 8080 {
		t.Fatalf("snapshot value = %+v, want decoded config", got)
	}
}

func TestLoadRunsOptionalPipelineStages(t *testing.T) {
	pipeline := testPipeline()
	pipeline.ApplyDefaults = func(ctx context.Context, value stepsTestConfig) (stepsTestConfig, error) {
		value.Port = 8080
		return value, nil
	}
	pipeline.ValidateConfig = func(ctx context.Context, value stepsTestConfig) error {
		if value.Port == 0 {
			return errors.New("missing port")
		}
		return nil
	}
	pipeline.Copy = func(ctx context.Context, value stepsTestConfig) (stepsTestConfig, error) {
		value.Name = "copied-" + value.Name
		return value, nil
	}
	pipeline.Redact = func(ctx context.Context, value stepsTestConfig) (configkit.RedactedView, error) {
		return configkit.RedactedView{"name": value.Name, "port": value.Port}, nil
	}

	result, err := configkit.Load(context.Background(), configkit.AttemptKindInitialLoad, configkit.SourceData{
		Data: []byte(`{"name":"api","enabled":true}`),
	}, pipeline)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	value := result.Snapshot.Value()
	if value.Name != "copied-api" {
		t.Fatalf("snapshot value name = %q, want copied-api", value.Name)
	}
	if value.Port != 8080 {
		t.Fatalf("snapshot value port = %d, want 8080", value.Port)
	}
	if got := result.Snapshot.Redacted()["name"]; got != "copied-api" {
		t.Fatalf("redacted name = %v, want copied-api", got)
	}
}

func TestLoadReturnsContextFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := configkit.Load(ctx, configkit.AttemptKindReload, configkit.SourceData{}, testPipeline())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("load canceled error = %v, want context.Canceled", err)
	}
	assertFailedAttempt(t, result, configkit.AttemptStageContext)
}

func TestLoadReturnsPipelineValidateFailure(t *testing.T) {
	result, err := configkit.Load(context.Background(), configkit.AttemptKindReload, configkit.SourceData{}, configkit.Pipeline[stepsTestConfig]{})
	if !errors.Is(err, configkit.ErrMissingDecoder) {
		t.Fatalf("load missing decoder error = %v, want configkit.ErrMissingDecoder", err)
	}
	assertFailedAttempt(t, result, configkit.AttemptStagePipelineValidate)
}

func TestLoadReturnsStageFailures(t *testing.T) {
	stageErr := errors.New("stage failed")

	tests := []struct {
		name     string
		pipeline configkit.Pipeline[stepsTestConfig]
		stage    configkit.AttemptStage
		wantText string
	}{
		{
			name: "decode",
			pipeline: configkit.Pipeline[stepsTestConfig]{
				Decode: func(ctx context.Context, data configkit.SourceData) (stepsTestConfig, error) {
					return stepsTestConfig{}, stageErr
				},
				Redact:   configkit.EmptyRedactor[stepsTestConfig](),
				Checksum: configkit.SHA256JSONChecksum[stepsTestConfig](),
			},
			stage:    configkit.AttemptStageDecode,
			wantText: "decode config: stage failed",
		},
		{
			name: "defaults",
			pipeline: withPipelineOverride(testPipeline(), func(p *configkit.Pipeline[stepsTestConfig]) {
				p.ApplyDefaults = func(ctx context.Context, value stepsTestConfig) (stepsTestConfig, error) {
					return stepsTestConfig{}, stageErr
				}
			}),
			stage:    configkit.AttemptStageDefaults,
			wantText: "apply config defaults: stage failed",
		},
		{
			name: "validate config",
			pipeline: withPipelineOverride(testPipeline(), func(p *configkit.Pipeline[stepsTestConfig]) {
				p.ValidateConfig = func(ctx context.Context, value stepsTestConfig) error {
					return stageErr
				}
			}),
			stage:    configkit.AttemptStageValidateConfig,
			wantText: "validate config: stage failed",
		},
		{
			name: "copy",
			pipeline: withPipelineOverride(testPipeline(), func(p *configkit.Pipeline[stepsTestConfig]) {
				p.Copy = func(ctx context.Context, value stepsTestConfig) (stepsTestConfig, error) {
					return stepsTestConfig{}, stageErr
				}
			}),
			stage:    configkit.AttemptStageCopy,
			wantText: "copy config: stage failed",
		},
		{
			name: "redact",
			pipeline: withPipelineOverride(testPipeline(), func(p *configkit.Pipeline[stepsTestConfig]) {
				p.Redact = func(ctx context.Context, value stepsTestConfig) (configkit.RedactedView, error) {
					return nil, stageErr
				}
			}),
			stage:    configkit.AttemptStageRedact,
			wantText: "redact config: stage failed",
		},
		{
			name: "checksum",
			pipeline: withPipelineOverride(testPipeline(), func(p *configkit.Pipeline[stepsTestConfig]) {
				p.Checksum = func(ctx context.Context, value stepsTestConfig) (string, error) {
					return "", stageErr
				}
			}),
			stage:    configkit.AttemptStageChecksum,
			wantText: "checksum config: stage failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := configkit.Load(context.Background(), configkit.AttemptKindReload, configkit.SourceData{
				Data: []byte(`{"name":"api","enabled":true,"port":8080}`),
			}, tt.pipeline)
			if err == nil {
				t.Fatal("load error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("load error = %q, want containing %q", err.Error(), tt.wantText)
			}
			assertFailedAttempt(t, result, tt.stage)
			if result.Attempt.Error != err.Error() {
				t.Fatalf("attempt error = %q, want %q", result.Attempt.Error, err.Error())
			}
		})
	}
}

func TestLoadRecoversPipelinePanic(t *testing.T) {
	panicErr := errors.New("boom")
	pipeline := testPipeline()
	pipeline.ValidateConfig = func(ctx context.Context, value stepsTestConfig) error {
		panic(panicErr)
	}

	result, err := configkit.Load(context.Background(), configkit.AttemptKindReload, configkit.SourceData{
		Data: []byte(`{"name":"api","enabled":true,"port":8080}`),
	}, pipeline)
	if err == nil {
		t.Fatal("load panic error = nil, want error")
	}
	if !errors.Is(err, panicErr) {
		t.Fatalf("load panic error = %v, want wrapping panic error", err)
	}
	if !strings.Contains(err.Error(), "validate config panic: boom") {
		t.Fatalf("load panic error = %q, want panic prefix", err.Error())
	}
	assertFailedAttempt(t, result, configkit.AttemptStageValidateConfig)
}

func TestLoadFromSourceUsesSourceMetadataFallback(t *testing.T) {
	sourceMetadata := configkit.SourceMetadata{Name: "source-name", Kind: "memory"}
	source := fakeSource{
		metadata: sourceMetadata,
		data: configkit.SourceData{
			Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
			Revision: "rev-1",
		},
	}

	result, err := configkit.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, source, testPipeline())
	if err != nil {
		t.Fatalf("load from source: %v", err)
	}
	if result.Attempt.Source != sourceMetadata {
		t.Fatalf("attempt source = %+v, want %+v", result.Attempt.Source, sourceMetadata)
	}
	if result.Snapshot.Metadata().Source != sourceMetadata {
		t.Fatalf("snapshot source = %+v, want %+v", result.Snapshot.Metadata().Source, sourceMetadata)
	}
}

func TestLoadFromSourcePreservesSourceDataMetadata(t *testing.T) {
	sourceMetadata := configkit.SourceMetadata{Name: "source-name", Kind: "memory"}
	dataMetadata := configkit.SourceMetadata{Name: "data-name", Kind: "merged"}
	source := fakeSource{
		metadata: sourceMetadata,
		data: configkit.SourceData{
			Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
			Metadata: dataMetadata,
			Revision: "rev-1",
		},
	}

	result, err := configkit.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, source, testPipeline())
	if err != nil {
		t.Fatalf("load from source: %v", err)
	}
	if result.Attempt.Source != dataMetadata {
		t.Fatalf("attempt source = %+v, want data metadata %+v", result.Attempt.Source, dataMetadata)
	}
}

func TestLoadFromSourceMissingSource(t *testing.T) {
	result, err := configkit.LoadFromSource(context.Background(), configkit.AttemptKindReload, nil, testPipeline())
	if !errors.Is(err, configkit.ErrMissingSource) {
		t.Fatalf("load missing source error = %v, want configkit.ErrMissingSource", err)
	}
	assertFailedAttempt(t, result, configkit.AttemptStageSourceRead)
}

func TestLoadFromSourceReadError(t *testing.T) {
	readErr := errors.New("read failed")
	source := fakeSource{
		metadata: configkit.SourceMetadata{Name: "source-name", Kind: "memory"},
		readErr:  readErr,
	}

	result, err := configkit.LoadFromSource(context.Background(), configkit.AttemptKindReload, source, testPipeline())
	if !errors.Is(err, readErr) {
		t.Fatalf("load source read error = %v, want readErr", err)
	}
	if !strings.Contains(err.Error(), "read config source: read failed") {
		t.Fatalf("load source read error = %q, want source read prefix", err.Error())
	}
	assertFailedAttempt(t, result, configkit.AttemptStageSourceRead)
	if result.Attempt.Source != source.metadata {
		t.Fatalf("attempt source = %+v, want %+v", result.Attempt.Source, source.metadata)
	}
}

func TestLoadFromSourceRecoversMetadataPanic(t *testing.T) {
	source := fakeSource{panicMetadata: true}

	result, err := configkit.LoadFromSource(context.Background(), configkit.AttemptKindReload, source, testPipeline())
	if err == nil {
		t.Fatal("load source metadata panic error = nil, want error")
	}
	if !strings.Contains(err.Error(), "read config source metadata panic") {
		t.Fatalf("load source metadata panic error = %q, want metadata panic prefix", err.Error())
	}
	assertFailedAttempt(t, result, configkit.AttemptStageSourceRead)
}

func TestLoadFromSourceRecoversReadPanic(t *testing.T) {
	source := fakeSource{
		metadata:  configkit.SourceMetadata{Name: "source-name", Kind: "memory"},
		panicRead: true,
	}

	result, err := configkit.LoadFromSource(context.Background(), configkit.AttemptKindReload, source, testPipeline())
	if err == nil {
		t.Fatal("load source read panic error = nil, want error")
	}
	if !strings.Contains(err.Error(), "read config source panic") {
		t.Fatalf("load source read panic error = %q, want read panic prefix", err.Error())
	}
	assertFailedAttempt(t, result, configkit.AttemptStageSourceRead)
}

type fakeSource struct {
	metadata      configkit.SourceMetadata
	data          configkit.SourceData
	readErr       error
	panicMetadata bool
	panicRead     bool
}

func (s fakeSource) Metadata() configkit.SourceMetadata {
	if s.panicMetadata {
		panic("metadata boom")
	}
	return s.metadata
}

func (s fakeSource) Read(ctx context.Context) (configkit.SourceData, error) {
	if s.panicRead {
		panic("read boom")
	}
	if s.readErr != nil {
		return configkit.SourceData{}, s.readErr
	}
	return s.data, nil
}

func withPipelineOverride(pipeline configkit.Pipeline[stepsTestConfig], override func(*configkit.Pipeline[stepsTestConfig])) configkit.Pipeline[stepsTestConfig] {
	override(&pipeline)
	return pipeline
}

func assertFailedAttempt(t *testing.T, result configkit.LoadResult[stepsTestConfig], stage configkit.AttemptStage) {
	t.Helper()

	if result.Snapshot != nil {
		t.Fatalf("snapshot = %+v, want nil", result.Snapshot)
	}
	if result.Attempt.Status != configkit.AttemptStatusFailed {
		t.Fatalf("attempt status = %q, want %q", result.Attempt.Status, configkit.AttemptStatusFailed)
	}
	if result.Attempt.Stage != stage {
		t.Fatalf("attempt stage = %q, want %q", result.Attempt.Stage, stage)
	}
	if result.Attempt.Error == "" {
		t.Fatal("attempt error = empty, want error")
	}
	if result.Attempt.StartedAt.IsZero() || result.Attempt.EndedAt.IsZero() {
		t.Fatalf("attempt times = %v/%v, want non-zero", result.Attempt.StartedAt, result.Attempt.EndedAt)
	}
}

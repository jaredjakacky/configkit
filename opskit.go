package configkit

import (
	"context"
	"strconv"
	"time"

	opskit "github.com/jaredjakacky/opskit"
)

const (
	defaultComponentName        = "config"
	defaultComponentKind        = "config"
	defaultComponentDescription = "application configuration"
)

var (
	_ opskit.Component            = (*Manager[any])(nil)
	_ opskit.ReadinessContributor = (*Manager[any])(nil)
	_ opskit.Inspector            = (*Manager[any])(nil)
)

// ComponentInfo returns the Opskit identity for this manager.
func (m *Manager[T]) ComponentInfo() opskit.ComponentInfo {
	return cloneOpsComponentInfo(managerComponentInfo(m.componentInfo, true))
}

// Status returns this manager's cached lifecycle state as an Opskit status.
//
// Status does not read sources, run the pipeline, or expose high-cardinality
// lifecycle details such as source names, revisions, checksums, or errors.
func (m *Manager[T]) Status(context.Context) opskit.Status {
	status := m.LifecycleStatus()
	ready := lifecycleReady(status.State, m.managerDegradedReady())

	return opskit.Status{
		State:      opskitStateFromLifecycleState(status.State),
		Ready:      ready,
		Message:    opskitStatusMessage(status.State, ready),
		UpdatedAt:  opskitStatusUpdatedAt(status),
		Attributes: opskitLifecycleAttributes(status),
	}
}

// Readiness returns this manager's configured Opskit readiness.
func (m *Manager[T]) Readiness(context.Context) opskit.Readiness {
	status := m.LifecycleStatus()
	ready := lifecycleReady(status.State, m.managerDegradedReady())
	info := m.ComponentInfo()
	reason := opskitReadinessReason(status.State, ready)

	return opskit.Readiness{
		Ready:  ready,
		Reason: reason,
		Components: []opskit.ReadinessItem{
			{
				Name:    info.Name,
				Kind:    info.Kind,
				Policy:  opskit.ReadinessRequired,
				Ready:   ready,
				State:   opskitStateFromLifecycleState(status.State),
				Reason:  reason,
				Message: opskitStatusMessage(status.State, ready),
			},
		},
	}
}

// Inspect returns a safe Opskit inspection view for this manager.
func (m *Manager[T]) Inspect(ctx context.Context) (opskit.Inspection, error) {
	if err := ctx.Err(); err != nil {
		return opskit.Inspection{}, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	status := m.statusLocked()
	var redacted RedactedView
	if m.current != nil {
		redacted = m.current.Redacted()
	}
	attempts := cloneAttemptRecords(m.attemptHistory)
	lastApply := cloneApplyResultPtr(m.lastApply)

	return m.opskitInspectionFromSnapshot(status, redacted, attempts, lastApply), nil
}

func (m *Manager[T]) opskitInspectionFromSnapshot(status LifecycleStatus, redacted RedactedView, attempts []AttemptRecord, lastApply *ApplyResult) opskit.Inspection {
	ready := lifecycleReady(status.State, m.managerDegradedReady())
	return opskit.Inspection{
		Summary: opskitInspectionSummary{
			State:       status.State,
			Ready:       ready,
			Current:     status.Current,
			LastAttempt: attemptSummary(status.LastAttempt),
			LastSuccess: attemptSummary(status.LastSuccess),
			LastFailure: attemptSummary(status.LastFailure),
		},
		Details: opskitInspectionDetails{
			Redacted:       redacted,
			RecentAttempts: attempts,
			LastApply:      lastApply,
		},
		Attributes: opskitLifecycleAttributes(status),
	}
}

type opskitInspectionSummary struct {
	State       LifecycleState      `json:"state"`
	Ready       bool                `json:"ready"`
	Current     *SnapshotMetadata   `json:"current,omitempty"`
	LastAttempt *attemptSummaryView `json:"last_attempt,omitempty"`
	LastSuccess *attemptSummaryView `json:"last_success,omitempty"`
	LastFailure *attemptSummaryView `json:"last_failure,omitempty"`
}

type attemptSummaryView struct {
	ID        uint64        `json:"id,omitempty"`
	Kind      AttemptKind   `json:"kind,omitempty"`
	Status    AttemptStatus `json:"status"`
	Stage     AttemptStage  `json:"stage,omitempty"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at"`
}

type opskitInspectionDetails struct {
	Redacted       RedactedView    `json:"redacted,omitempty"`
	RecentAttempts []AttemptRecord `json:"recent_attempts,omitempty"`
	LastApply      *ApplyResult    `json:"last_apply,omitempty"`
}

func managerComponentInfo(info opskit.ComponentInfo, set bool) opskit.ComponentInfo {
	out := opskit.ComponentInfo{
		Name:        defaultComponentName,
		Kind:        defaultComponentKind,
		Description: defaultComponentDescription,
		Labels: []opskit.Attribute{
			opskit.Attr("kit", "configkit"),
		},
	}
	if !set {
		return out
	}
	if info.Name != "" {
		out.Name = info.Name
	}
	if info.Kind != "" {
		out.Kind = info.Kind
	}
	if info.Description != "" {
		out.Description = info.Description
	}
	if len(info.Labels) > 0 {
		out.Labels = mergeOpsComponentLabels(info.Labels)
	}
	return out
}

func cloneOpsComponentInfo(info opskit.ComponentInfo) opskit.ComponentInfo {
	info.Labels = cloneOpsAttributes(info.Labels)
	return info
}

func cloneOpsAttributes(attrs []opskit.Attribute) []opskit.Attribute {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]opskit.Attribute, len(attrs))
	copy(out, attrs)
	return out
}

func mergeOpsComponentLabels(labels []opskit.Attribute) []opskit.Attribute {
	out := []opskit.Attribute{opskit.Attr("kit", "configkit")}
	for _, label := range labels {
		if label.Key == "kit" {
			continue
		}
		out = append(out, label)
	}
	return out
}

func (m *Manager[T]) managerDegradedReady() bool {
	if !m.degradedReadySet {
		return true
	}
	return m.degradedReady
}

func opskitStateFromLifecycleState(state LifecycleState) opskit.State {
	switch state {
	case LifecycleStateUnloaded:
		return opskit.StateNotReady
	case LifecycleStateFailed:
		return opskit.StateFailed
	case LifecycleStateLoaded:
		return opskit.StateReady
	case LifecycleStateDegraded:
		return opskit.StateDegraded
	default:
		return opskit.StateUnknown
	}
}

func lifecycleReady(state LifecycleState, degradedReady bool) bool {
	switch state {
	case LifecycleStateLoaded:
		return true
	case LifecycleStateDegraded:
		return degradedReady
	default:
		return false
	}
}

func opskitStatusMessage(state LifecycleState, ready bool) string {
	switch state {
	case LifecycleStateUnloaded:
		return "config not loaded"
	case LifecycleStateFailed:
		return "config load failed"
	case LifecycleStateLoaded:
		return "config loaded"
	case LifecycleStateDegraded:
		if ready {
			return "config degraded: using last known good"
		}
		return "config degraded"
	default:
		return "config state unknown"
	}
}

func opskitReadinessReason(state LifecycleState, ready bool) string {
	switch state {
	case LifecycleStateUnloaded:
		return "config not loaded"
	case LifecycleStateFailed:
		return "config load failed"
	case LifecycleStateLoaded:
		return "config loaded"
	case LifecycleStateDegraded:
		if ready {
			return "config degraded: using last known good"
		}
		return "config degraded"
	default:
		return "config state unknown"
	}
}

func opskitStatusUpdatedAt(status LifecycleStatus) *time.Time {
	if status.LastAttempt != nil && !status.LastAttempt.EndedAt.IsZero() {
		updatedAt := status.LastAttempt.EndedAt
		return &updatedAt
	}
	if status.Current != nil && !status.Current.LoadedAt.IsZero() {
		updatedAt := status.Current.LoadedAt
		return &updatedAt
	}
	return nil
}

func opskitLifecycleAttributes(status LifecycleStatus) []opskit.Attribute {
	attrs := []opskit.Attribute{
		opskit.Attr("config_state", string(status.State)),
		opskit.Attr("has_current", strconv.FormatBool(status.Current != nil)),
	}
	if sourceKind := opskitLifecycleSourceKind(status); sourceKind != "" {
		attrs = append(attrs, opskit.Attr("source_kind", sourceKind))
	}
	if status.LastAttempt != nil {
		if status.LastAttempt.Kind != "" {
			attrs = append(attrs, opskit.Attr("last_attempt_kind", string(status.LastAttempt.Kind)))
		}
		if status.LastAttempt.Status != "" {
			attrs = append(attrs, opskit.Attr("last_attempt_status", string(status.LastAttempt.Status)))
		}
		if status.LastAttempt.Stage != "" {
			attrs = append(attrs, opskit.Attr("last_attempt_stage", string(status.LastAttempt.Stage)))
		}
	}
	return attrs
}

func opskitLifecycleSourceKind(status LifecycleStatus) string {
	if status.Current != nil && status.Current.Source.Kind != "" {
		return status.Current.Source.Kind
	}
	if status.LastAttempt != nil {
		return status.LastAttempt.Source.Kind
	}
	return ""
}

func attemptSummary(attempt *AttemptRecord) *attemptSummaryView {
	if attempt == nil {
		return nil
	}
	return &attemptSummaryView{
		ID:        attempt.ID,
		Kind:      attempt.Kind,
		Status:    attempt.Status,
		Stage:     attempt.Stage,
		StartedAt: attempt.StartedAt,
		EndedAt:   attempt.EndedAt,
	}
}

package taskbench

// All costs are configured settled costs, distinct from supplier billing and
// permanent reservations. Both currency axes are always separate.
type windowV8RuntimeSummary struct {
	Schema              string                  `json:"schema"`
	LogicalModelCalls   int                     `json:"logical_model_calls"`
	LogicalJEVCalls     int                     `json:"logical_jev_calls"`
	NativeModelIntents  int                     `json:"native_model_intents"`
	NativeJEVIntents    int                     `json:"native_jev_intents"`
	ModelRetryIntents   int                     `json:"model_retry_intents"`
	JEVRetryIntents     int                     `json:"jev_retry_intents"`
	NativeSettlements   int                     `json:"native_settlements"`
	NativeUnknown       int                     `json:"native_unknown"`
	NativePending       int                     `json:"native_pending"`
	RetryRequired       bool                    `json:"retry_required"`
	Halt                string                  `json:"halt"`
	Known               windowV8Amounts         `json:"configured_known"`
	UnknownHeld         windowV8Amounts         `json:"unknown_held"`
	PendingHeld         windowV8Amounts         `json:"pending_held"`
	HistoricalPermanent windowV8Amounts         `json:"historical_permanent"`
	BasePermanent       windowV8Amounts         `json:"base_permanent"`
	ExtraPermanent      windowV8Amounts         `json:"extra_permanent"`
	TotalPermanent      windowV8Amounts         `json:"total_permanent"`
	UnknownIntents      []windowV8UnknownRecord `json:"unknown_intents"`
	Extras              []windowV8ExtraClaim    `json:"extras"`
}
type windowV8UnknownRecord struct {
	WindowID string `json:"window_id"`
	windowUnknownIntent
}

func (c *windowCampaign) snapshotWindowV8Runtime(state windowAccountingState, windowID string) *windowV8RuntimeSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotWindowV8RuntimeLocked(state, windowID)
}
func (c *windowCampaign) snapshotWindowV8RuntimeLocked(state windowAccountingState, windowID string) *windowV8RuntimeSummary {
	if !windowScopeUsesNativeV8(c.prepared.scope.Schema) {
		return nil
	}
	out := &windowV8RuntimeSummary{Schema: "full-window-runtime-evidence.v8", NativeModelIntents: state.ModelAttempts, NativeJEVIntents: state.JEVAttempts, NativeUnknown: len(state.UnknownIntents), RetryRequired: state.RetryRequired != nil, Halt: state.Halt, Known: windowV8Amounts{state.ModelKnown, state.JEVKnown}, UnknownHeld: windowV8Amounts{state.ModelUnknownHeld, state.JEVUnknownHeld}, PendingHeld: windowV8Amounts{state.ModelHeld, state.JEVHeld}, HistoricalPermanent: windowV8Amounts{c.baseline.PermanentModel, c.baseline.PermanentJEV}, ExtraPermanent: windowV8Amounts{int64(c.modelExtras) * 74752, int64(c.jevExtras) * 2688}, TotalPermanent: windowV8Amounts{c.model, c.jev}, UnknownIntents: []windowV8UnknownRecord{}, Extras: append([]windowV8ExtraClaim{}, c.extras...)}
	for _, u := range state.UnknownIntents {
		out.UnknownIntents = append(out.UnknownIntents, windowV8UnknownRecord{windowID, u})
		if u.Request.Provider == "model" {
			out.ModelRetryIntents++
		} else {
			out.JEVRetryIntents++
		}
	}
	// RetryRequired survives while a funded native2 is pending/failed. Only an
	// unresolved unknown with no native2 pending has not yet created that intent.
	if state.RetryRequired != nil && state.Pending == nil {
		if state.RetryRequired.Request.Provider == "model" {
			out.ModelRetryIntents--
		} else {
			out.JEVRetryIntents--
		}
	}
	if state.Pending != nil {
		out.NativePending = 1
	}
	out.LogicalModelCalls = state.ModelAttempts - out.ModelRetryIntents
	out.LogicalJEVCalls = state.JEVAttempts - out.JEVRetryIntents
	out.NativeSettlements = state.ModelAttempts + state.JEVAttempts - out.NativeUnknown - out.NativePending
	out.BasePermanent = windowV8Amounts{c.model - c.baseline.PermanentModel - out.ExtraPermanent.Model, c.jev - c.baseline.PermanentJEV - out.ExtraPermanent.JEV}
	return out
}
func mergeWindowV8Runtime(prior, next *windowV8RuntimeSummary) *windowV8RuntimeSummary {
	if next == nil {
		return prior
	}
	if prior == nil {
		return next
	}
	out := *next
	out.LogicalModelCalls += prior.LogicalModelCalls
	out.LogicalJEVCalls += prior.LogicalJEVCalls
	out.NativeModelIntents += prior.NativeModelIntents
	out.NativeJEVIntents += prior.NativeJEVIntents
	out.ModelRetryIntents += prior.ModelRetryIntents
	out.JEVRetryIntents += prior.JEVRetryIntents
	out.NativeSettlements += prior.NativeSettlements
	out.NativeUnknown += prior.NativeUnknown
	out.NativePending += prior.NativePending
	out.Known.Model += prior.Known.Model
	out.Known.JEV += prior.Known.JEV
	out.UnknownHeld.Model += prior.UnknownHeld.Model
	out.UnknownHeld.JEV += prior.UnknownHeld.JEV
	out.PendingHeld.Model += prior.PendingHeld.Model
	out.PendingHeld.JEV += prior.PendingHeld.JEV
	out.UnknownIntents = append(append([]windowV8UnknownRecord{}, prior.UnknownIntents...), next.UnknownIntents...)
	return &out
}

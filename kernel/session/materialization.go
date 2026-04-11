package session

// MarkEventMaterialized marks an event as already applied in sess's current
// materialization domain so outer orchestration layers can skip duplicate
// commits to the same shared session.
func MarkEventMaterialized(event *Event, sess *Session) {
	if event == nil || sess == nil {
		return
	}
	event.Actions.MaterializedIn = sess.MaterializationDomain()
}

// EventMaterializedIn reports whether event has already been committed in the
// given session's materialization domain.
func EventMaterializedIn(event *Event, sess *Session) bool {
	if event == nil || sess == nil {
		return false
	}
	return event.Actions.MaterializedIn != "" &&
		event.Actions.MaterializedIn == sess.MaterializationDomain()
}

// MaterializeEvent applies a yielded event's shared effects to sess exactly
// once per materialization domain. Partial events are intentionally ignored.
func MaterializeEvent(sess *Session, event *Event) {
	if sess == nil || event == nil || event.Partial || EventMaterializedIn(event, sess) {
		return
	}
	if event.Content != nil {
		sess.AppendMessage(CloneMessage(*event.Content))
	}
	for key, value := range event.Actions.StateDelta {
		sess.SetState(key, value)
	}
	if event.Type == EventTypeLLMResponse {
		sess.Budget.Record(event.Usage.TotalTokens, 1)
	}
	MarkEventMaterialized(event, sess)
}

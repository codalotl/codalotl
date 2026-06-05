package llmstream

import (
	"sort"
	"sync"
)

// DiagnosticHookReceiver receives AddTurn calls with a request/response pair. The request is the JSON-ish into, for instance, OpenAI's /v1/responses. The response
// is the completed response object (or potentially, an error object). Even though responses are streamed, the `response` here represents the completed object, as
// if there was no streaming (ex: `{"id": "resp_123", "object": "response", ...}`).
//
// This method may be called eagerly as soon as we know the response object, but must be called before the channel returned by SendAsync closes.
type DiagnosticHookReceiver interface {
	// AddTurn records one provider turn with its request payload and completed response or error payload.
	AddTurn(request map[string]any, response map[string]any)
}

var (
	diagnosticHooksMu    sync.RWMutex
	nextDiagnosticHookID int
	diagnosticHooks      = make(map[int]DiagnosticHookReceiver)
)

// AddDiagnosticHook adds recv to a list of hook receivers, which will be called when a turn is complete (we have a request/response pair). It returns an unregister
// function that removes this hook. The unregister function is safe to call multiple times.
func AddDiagnosticHook(recv DiagnosticHookReceiver) (unregister func()) {
	if recv == nil {
		return func() {}
	}

	diagnosticHooksMu.Lock()
	nextDiagnosticHookID++
	id := nextDiagnosticHookID
	diagnosticHooks[id] = recv
	diagnosticHooksMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			diagnosticHooksMu.Lock()
			delete(diagnosticHooks, id)
			diagnosticHooksMu.Unlock()
		})
	}
}

func hasDiagnosticHooks() bool {
	diagnosticHooksMu.RLock()
	defer diagnosticHooksMu.RUnlock()
	return len(diagnosticHooks) > 0
}

func emitDiagnosticTurn(request map[string]any, response map[string]any) {
	if request == nil || response == nil {
		return
	}

	for _, recv := range snapshotDiagnosticHooks() {
		recv.AddTurn(request, response)
	}
}

// snapshotDiagnosticHooks returns a registration-ordered snapshot of diagnostic hook receivers.
func snapshotDiagnosticHooks() []DiagnosticHookReceiver {
	diagnosticHooksMu.RLock()
	defer diagnosticHooksMu.RUnlock()

	if len(diagnosticHooks) == 0 {
		return nil
	}

	ids := make([]int, 0, len(diagnosticHooks))
	for id := range diagnosticHooks {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	receivers := make([]DiagnosticHookReceiver, 0, len(ids))
	for _, id := range ids {
		receivers = append(receivers, diagnosticHooks[id])
	}
	return receivers
}

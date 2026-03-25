package llmstream

import (
	"sort"
	"sync"
)

// DiagnosticHookReceiver receives request/response pairs for completed provider
// turns.
type DiagnosticHookReceiver interface {
	AddTurn(request map[string]any, response map[string]any)
}

var (
	diagnosticHooksMu    sync.RWMutex
	nextDiagnosticHookID int
	diagnosticHooks      = make(map[int]DiagnosticHookReceiver)
)

// AddDiagnosticHook registers recv and returns an unregister function. The
// returned function is safe to call multiple times.
func AddDiagnosticHook(recv DiagnosticHookReceiver) func() {
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

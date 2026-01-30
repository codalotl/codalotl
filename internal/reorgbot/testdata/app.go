package mypkg

// Unattached comment above AppConfig

// AppConfig holds configuration for the dummy app.
type AppConfig struct {
	Name       string
	MaxWorkers int
	EnableLogs bool
}

// Worker represents a unit that can process tasks.
type Worker struct {
	ID   int
	Busy bool
}

// App is a small, reorganizable application with workers.
type App struct {
	cfg     AppConfig
	workers []Worker
}

// NewApp constructs a new App with the given configuration and worker count.
func NewApp(cfg AppConfig) *App {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 1
	}
	workers := make([]Worker, cfg.MaxWorkers)
	for i := 0; i < cfg.MaxWorkers; i++ {
		workers[i] = Worker{ID: i + 1}
	}
	return &App{cfg: cfg, workers: workers}
}

// NumIdleWorkers returns the number of workers that are not busy.
func (a *App) NumIdleWorkers() int {
	idle := 0
	for i := range a.workers {
		if !a.workers[i].Busy {
			idle++
		}
	}
	return idle
}

// SetBusy marks a worker busy or idle by id. Returns true if a worker was found.
func (a *App) SetBusy(workerID int, busy bool) bool {
	for i := range a.workers {
		if a.workers[i].ID == workerID {
			a.workers[i].Busy = busy
			return true
		}
	}
	return false
}

// EachWorker applies fn to each worker.
func (a *App) EachWorker(fn func(w *Worker)) {
	for i := range a.workers {
		fn(&a.workers[i])
	}
}

// Name returns the configured application name.
func (a *App) Name() string { return a.cfg.Name }

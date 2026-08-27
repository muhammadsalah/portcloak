package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"portcloak/internal/engine/obs"
)

// EventProgress is the single Wails event name every job progress message
// travels on. The frontend subscribes once and routes by job id and kind.
const EventProgress = "portcloak:progress"

// eventBridge carries engine events onto the Wails event bus.
//
// It is the only place the two worlds meet. The engine emits into an obs.Sink
// and knows nothing about Wails; this adapter is what a headless test replaces
// with a recorder.
type eventBridge struct {
	app *application.App
}

func newEventBridge(app *application.App) obs.Sink {
	return &eventBridge{app: app}
}

func (b *eventBridge) Emit(e obs.Event) {
	b.app.Event.Emit(EventProgress, e)
}

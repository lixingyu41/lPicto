package debugcontrol

import (
	"errors"
	"sync/atomic"
)

var ErrBackgroundProcessingPaused = errors.New("background processing paused by debug settings")
var ErrExternalFileAccessPaused = errors.New("external file access paused by debug settings")

var externalFileAccessPaused atomic.Bool
var backgroundProcessingPaused atomic.Bool

func Apply(externalPaused, backgroundPaused bool) {
	externalFileAccessPaused.Store(externalPaused)
	backgroundProcessingPaused.Store(backgroundPaused)
}

func ExternalFileAccessPaused() bool { return externalFileAccessPaused.Load() }

// BackgroundProcessingPaused is effective state: cutting external storage also
// pauses every worker that could otherwise open a source file.
func BackgroundProcessingPaused() bool {
	return externalFileAccessPaused.Load() || backgroundProcessingPaused.Load()
}

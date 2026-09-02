package debugcontrol

import "testing"

func TestEffectiveBackgroundPause(t *testing.T) {
	defer Apply(false, false)
	Apply(false, false)
	if BackgroundProcessingPaused() || ExternalFileAccessPaused() {
		t.Fatal("expected controls to be enabled")
	}
	Apply(false, true)
	if !BackgroundProcessingPaused() || ExternalFileAccessPaused() {
		t.Fatal("background-only pause is incorrect")
	}
	Apply(true, false)
	if !BackgroundProcessingPaused() || !ExternalFileAccessPaused() {
		t.Fatal("external pause must imply background pause")
	}
}

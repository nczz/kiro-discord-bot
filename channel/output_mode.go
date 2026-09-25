package channel

// OutputMode controls how agent progress is shown in Discord.
type OutputMode string

const (
	// OutputModeFull shows detailed tool progress and output.
	OutputModeFull OutputMode = "full"
	// OutputModeCompact shows compact tool starts and failure details.
	OutputModeCompact OutputMode = "compact"
	// OutputModeFolded edits one progress card instead of posting each step.
	OutputModeFolded OutputMode = "folded"
)

// DefaultOutputMode is used for targets without an explicit output-mode setting.
const DefaultOutputMode = OutputModeFolded

// NormalizeOutputMode returns a supported output mode, preserving folded as the default.
func NormalizeOutputMode(mode OutputMode) OutputMode {
	switch mode {
	case OutputModeFull, OutputModeCompact, OutputModeFolded:
		return mode
	default:
		return DefaultOutputMode
	}
}

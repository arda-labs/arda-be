// Package extractor talks to an OpenAI-compatible vision model. It knows
// nothing about any document type.
package extractor

// Provider presets. A preset only changes request headers.
const (
	PresetOpenAICompatible = "openai-compatible"
	// PresetOpenCodeGo requires an x-opencode-session header; without it the
	// gateway answers 400 MissingSessionID.
	PresetOpenCodeGo = "opencode-go"
)

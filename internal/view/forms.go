package view

// SettingsForm carries submitted values as strings (so invalid input is echoed back
// unchanged) and per-field error messages, both keyed by field name.
type SettingsForm struct {
	Values map[string]string
	Errors map[string]string
}

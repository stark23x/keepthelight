package main

// dialogResult is what the Browse helper process answers (dialoghelper_windows.go).
type dialogResult struct {
	Path  string   `json:"path,omitempty"`
	Paths []string `json:"paths,omitempty"`
	Error string   `json:"error,omitempty"`
}

const dialogCrashMessage = "The Browse window crashed, so nothing was chosen; Keep the Light itself is fine. " +
	"This is usually an add-on to File Explorer that loads into every Open window (ExplorerPatcher's \"Register as shell " +
	"extension\" did this once). Windows names it: Event Viewer, Windows Logs, Application, the \"Application Error\" " +
	"entry, \"Faulting module name\". You can also type or paste the path instead."

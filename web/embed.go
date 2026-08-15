// Package web bettet alle UI-Assets ins Binary ein — die Oberfläche
// funktioniert ohne Internetzugang (Spec §3).
package web

import "embed"

//go:embed templates static
var FS embed.FS

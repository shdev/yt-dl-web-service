package web_test

import (
	"testing"

	"ytdlweb/web"
)

func TestEmbeddedAssets(t *testing.T) {
	files := []string{
		"templates/index.html",
		"static/app.js",
		"static/bootstrap.min.css",
		"static/bootstrap.bundle.min.js",
	}
	for _, f := range files {
		data, err := web.FS.ReadFile(f)
		if err != nil {
			t.Fatalf("%s fehlt im Embed: %v", f, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s ist leer", f)
		}
	}
}

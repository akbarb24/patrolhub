package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// templ escapes everything by default; templ.Raw would switch that off for user input.
func TestTemplatesNeverUseRawOutput(t *testing.T) {
	files, err := filepath.Glob("*.templ")
	if err != nil || len(files) == 0 {
		t.Fatalf("no templates found: %v", err)
	}
	for _, f := range files {
		src, _ := os.ReadFile(f)
		for _, banned := range []string{"templ.Raw", "templ.SafeScript", "@templ.Unsafe"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s uses %s", f, banned)
			}
		}
	}
}

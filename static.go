package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// payloadScript marshals v to JSON and wraps it as an inline script that sets a
// window global. json.Marshal HTML-escapes <, > and & by default, so the JSON
// cannot break out of the surrounding <script> tag.
func payloadScript(name string, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("error producing %s JSON: %w", name, err)
	}
	return fmt.Sprintf("<script>window.%s = %s</script>", name, string(b)), nil
}

// generateStaticHTML reads the embedded singlefile index.html (its JS/CSS
// bundle is already inlined by vite-plugin-singlefile) and writes a single
// self-contained file with the graph/map/rso payloads injected as window
// globals immediately before </head>. The Vue UI reads these globals in
// standalone mode (see ui/src/App.vue standaloneGlobals()).
func (r *ponto) generateStaticHTML(fe fs.FS, filename string) error {
	raw, err := fs.ReadFile(fe, "index.html")
	if err != nil {
		return fmt.Errorf("unable to read embedded index.html: %w", err)
	}

	parts := strings.SplitN(string(raw), "</head>", 2)
	if len(parts) != 2 {
		return fmt.Errorf("embedded index.html has no </head>; cannot inject payloads")
	}

	var scripts strings.Builder
	for _, p := range []struct {
		name string
		v    any
	}{
		{"graph", r.Graph},
		{"map", r.Map},
		{"rso", r.RSO},
	} {
		s, err := payloadScript(p.name, p.v)
		if err != nil {
			return err
		}
		scripts.WriteString(s)
	}

	content := parts[0] + scripts.String() + "</head>" + parts[1]
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		return fmt.Errorf("unable to write %s: %w", filename, err)
	}
	return nil
}

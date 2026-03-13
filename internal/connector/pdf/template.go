package pdf

import (
	"bytes"
	"fmt"
	"text/template"
)

// renderTemplate processes a Go template string with the given data.
func renderTemplate(tmpl string, data map[string]interface{}) (string, error) {
	t, err := template.New("pdf").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execute error: %w", err)
	}

	return buf.String(), nil
}

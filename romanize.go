package ichiran

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// RomanizeKana romanizes kana readings, such as dictionary forms, with
// Ichiran's own word romanizer, in one call. An empty reading gives an
// empty result.
func (im *IchiranManager) RomanizeKana(ctx context.Context, readings []string) ([]string, error) {
	if len(readings) == 0 {
		return nil, nil
	}

	var b strings.Builder
	b.WriteString(`(progn`)
	b.WriteString(` (ql:quickload :jsown :silent t)`)
	b.WriteString(` (handler-case`)
	b.WriteString(` (jsown:to-json (jsown:new-js ("adapterVersion" 1) ("results" (mapcar (lambda (reading)`)
	b.WriteString(` (if (plusp (length reading)) (ichiran:romanize-word (copy-seq reading)) "")) '(`)
	for _, reading := range readings {
		b.WriteString(fmt.Sprintf(`"%s" `, escapeLispString(reading)))
	}
	b.WriteString(`)))))`)
	b.WriteString(` (error (e) (jsown:to-json (jsown:new-js ("adapterError" (princ-to-string e))))))`)
	b.WriteString(`)`)

	output, err := im.runLispJSON(ctx, b.String())
	if err != nil {
		return nil, err
	}
	var envelope struct {
		AdapterVersion int      `json:"adapterVersion"`
		AdapterError   string   `json:"adapterError"`
		Results        []string `json:"results"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("invalid romanization envelope: %w", err)
	}
	if envelope.AdapterError != "" {
		return nil, fmt.Errorf("Ichiran romanization failed: %s", envelope.AdapterError)
	}
	if envelope.AdapterVersion != adapterVersion {
		return nil, fmt.Errorf("unsupported adapter version %d", envelope.AdapterVersion)
	}
	if len(envelope.Results) != len(readings) {
		return nil, fmt.Errorf("romanization returned %d results for %d readings", len(envelope.Results), len(readings))
	}
	return envelope.Results, nil
}

// EngineImage returns the ID of the image the Ichiran container runs, the
// provenance of an analysis. It names a build, not a dictionary version.
func (im *IchiranManager) EngineImage(ctx context.Context) (string, error) {
	client, err := im.docker.GetClient()
	if err != nil {
		return "", fmt.Errorf("failed to get Docker client: %w", err)
	}
	info, err := im.inspectContainer(ctx, client)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}
	return info.Image, nil
}

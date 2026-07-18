package traefik_plugin_state_geo

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"strings"
)

const (
	maxBlockTemplateBytes = 1 << 20
	defaultBlockPageHTML  = "<h1>Access Denied</h1><p>State: {{.State}}</p>"
)

type blockPageData struct {
	State string
}

type blockPageRenderer struct {
	primary  *template.Template
	fallback *template.Template
}

func newBlockPageRenderer(config *Config) (*blockPageRenderer, error) {
	fallback, err := template.New("default-block-page").Parse(defaultBlockPageHTML)
	if err != nil {
		return nil, fmt.Errorf("parse built-in block page: %w", err)
	}

	if config.TemplateHTML != "" && config.TemplatePath != "" {
		return nil, fmt.Errorf("templateHTML and templatePath cannot both be configured")
	}

	content := config.TemplateHTML
	if config.TemplatePath != "" {
		content, err = readBlockTemplate(config.TemplatePath)
		if err != nil {
			return nil, err
		}
	}
	if content == "" {
		return &blockPageRenderer{primary: fallback, fallback: fallback}, nil
	}
	if len(content) > maxBlockTemplateBytes {
		return nil, fmt.Errorf("block page template exceeds %d bytes", maxBlockTemplateBytes)
	}

	// Preserve the published placeholder while using html/template escaping.
	content = strings.ReplaceAll(content, "{{STATE}}", "{{.State}}")
	primary, err := template.New("custom-block-page").Parse(content)
	if err != nil {
		return nil, fmt.Errorf("parse block page template: %w", err)
	}

	return &blockPageRenderer{primary: primary, fallback: fallback}, nil
}

func readBlockTemplate(path string) (string, error) {
	// #nosec G304 -- the path is an explicit administrator-controlled setting.
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open block page template: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	content, err := io.ReadAll(io.LimitReader(file, maxBlockTemplateBytes+1))
	if err != nil {
		return "", fmt.Errorf("read block page template: %w", err)
	}
	if len(content) > maxBlockTemplateBytes {
		return "", fmt.Errorf("block page template exceeds %d bytes", maxBlockTemplateBytes)
	}

	return string(content), nil
}

func (r *blockPageRenderer) render(state string) ([]byte, error) {
	data := blockPageData{State: state}
	var output bytes.Buffer
	err := r.primary.Execute(&output, data)
	if err == nil {
		return output.Bytes(), nil
	}

	primaryErr := err
	output.Reset()
	if fallbackErr := r.fallback.Execute(&output, data); fallbackErr != nil {
		return []byte("<h1>Access Denied</h1>"), errors.Join(
			fmt.Errorf("render block page: %w", primaryErr),
			fmt.Errorf("render fallback: %w", fallbackErr),
		)
	}
	return output.Bytes(), fmt.Errorf("render block page: %w", primaryErr)
}

package traefik_plugin_state_geo

import (
	"os"
	"strings"
	"testing"
)

func TestBlockPageRendererEscapesState(t *testing.T) {
	config := CreateConfig()
	config.TemplateHTML = `<p>{{STATE}}</p>`

	renderer, err := newBlockPageRenderer(config)
	if err != nil {
		t.Fatalf("newBlockPageRenderer() error = %v", err)
	}
	body, err := renderer.render(`<script>alert("x")</script>`)
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}

	rendered := string(body)
	if strings.Contains(rendered, "<script>") {
		t.Fatalf("rendered state was not escaped: %s", rendered)
	}
	if !strings.Contains(rendered, "&lt;script&gt;") {
		t.Fatalf("rendered state does not contain escaped markup: %s", rendered)
	}
}

func TestBlockPageRendererLoadsTemplatePath(t *testing.T) {
	path := t.TempDir() + "/blocked.html"
	if err := os.WriteFile(path, []byte(`<p>FILE {{.State}}</p>`), 0o600); err != nil {
		t.Fatalf("write template fixture: %v", err)
	}

	config := CreateConfig()
	config.TemplatePath = path
	renderer, err := newBlockPageRenderer(config)
	if err != nil {
		t.Fatalf("newBlockPageRenderer() error = %v", err)
	}
	body, err := renderer.render("CA")
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}
	if string(body) != "<p>FILE CA</p>" {
		t.Fatalf("rendered body = %q, want %q", body, "<p>FILE CA</p>")
	}
}

func TestBlockPageRendererRejectsInvalidConfiguration(t *testing.T) {
	oversizedPath := t.TempDir() + "/oversized.html"
	if err := os.WriteFile(
		oversizedPath,
		[]byte(strings.Repeat("x", maxBlockTemplateBytes+1)),
		0o600,
	); err != nil {
		t.Fatalf("write oversized template fixture: %v", err)
	}

	tests := []struct {
		name      string
		configure func(*Config)
	}{
		{
			name: "inline and path both configured",
			configure: func(config *Config) {
				config.TemplateHTML = "inline"
				config.TemplatePath = "file.html"
			},
		},
		{
			name: "missing template path",
			configure: func(config *Config) {
				config.TemplatePath = "/does/not/exist/blocked.html"
			},
		},
		{
			name: "invalid template syntax",
			configure: func(config *Config) {
				config.TemplateHTML = "{{"
			},
		},
		{
			name: "oversized inline template",
			configure: func(config *Config) {
				config.TemplateHTML = strings.Repeat("x", maxBlockTemplateBytes+1)
			},
		},
		{
			name: "oversized file template",
			configure: func(config *Config) {
				config.TemplatePath = oversizedPath
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := CreateConfig()
			test.configure(config)
			if _, err := newBlockPageRenderer(config); err == nil {
				t.Fatal("newBlockPageRenderer() error = nil, want validation error")
			}
		})
	}
}

func TestBlockPageRendererFallsBackOnExecutionError(t *testing.T) {
	config := CreateConfig()
	config.TemplateHTML = `{{call .State}}`
	renderer, err := newBlockPageRenderer(config)
	if err != nil {
		t.Fatalf("newBlockPageRenderer() error = %v", err)
	}

	body, renderErr := renderer.render("CA")
	if renderErr == nil {
		t.Fatal("render() error = nil, want custom template execution error")
	}
	if !strings.Contains(string(body), "Access Denied") || !strings.Contains(string(body), "CA") {
		t.Fatalf("fallback body = %q, want built-in denied page", body)
	}
}

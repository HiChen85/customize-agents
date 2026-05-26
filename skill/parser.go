package skill

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Trigger     string `yaml:"trigger"`
}

func ParseFrontmatter(data []byte) (Frontmatter, string, error) {
	var fm Frontmatter

	if !bytes.HasPrefix(data, []byte("---\n")) {
		return fm, string(data), nil
	}

	rest := data[4:]
	endIdx := bytes.Index(rest, []byte("\n---\n"))
	if endIdx == -1 {
		return fm, "", fmt.Errorf("unterminated frontmatter")
	}

	fmData := rest[:endIdx]
	body := rest[endIdx+5:]

	if err := yaml.Unmarshal(fmData, &fm); err != nil {
		return fm, "", fmt.Errorf("parse frontmatter YAML: %w", err)
	}

	// Trim a single leading newline (the blank line conventionally separating frontmatter from body)
	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}

	return fm, string(body), nil
}

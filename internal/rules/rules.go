package rules

import (
	"os"
	"path/filepath"
	"strings"
)

func LoadRules(dir string) (string, error) {
	var sb strings.Builder

	files, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".md" {
			content, err := os.ReadFile(filepath.Join(dir, file.Name()))
			if err != nil {
				continue
			}
			sb.WriteString("\n--- Rule: " + file.Name() + " ---\n")
			sb.Write(content)
		}
	}

	return sb.String(), nil
}

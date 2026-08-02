package eval

import (
	"os"
	"strings"
)

func writeRaw(path, body string) error { return os.WriteFile(path, []byte(body), 0o644) }
func contains(s, sub string) bool      { return strings.Contains(s, sub) }

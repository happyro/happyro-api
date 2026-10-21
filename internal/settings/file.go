package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const blockStart = "// HAPPYRO ADMIN MANAGED START"
const blockEnd = "// HAPPYRO ADMIN MANAGED END"

// Render preserves operator configuration and replaces the final managed block.
// The caller serializes revisions before publishing, including across processes.
func Render(content string, changes map[string]int64) (string, error) {
	if err := Validate(changes, false); err != nil {
		return "", err
	}
	start, end := strings.Index(content, blockStart), strings.Index(content, blockEnd)
	if (start < 0) != (end < 0) || strings.Count(content, blockStart) > 1 || strings.Count(content, blockEnd) > 1 || end < start {
		return "", errors.New("invalid managed configuration markers")
	}
	values := map[string]int64{}
	prefix := content
	if start >= 0 {
		prefix = content[:start]
		if strings.TrimSpace(content[end+len(blockEnd):]) != "" {
			return "", errors.New("managed configuration must be the final block")
		}
		for _, line := range strings.Split(content[start+len(blockStart):end], "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			key, raw, ok := strings.Cut(line, ":")
			key = strings.TrimSpace(key)
			value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
			if !ok || err != nil {
				return "", errors.New("invalid managed configuration entry")
			}
			if _, exists := values[key]; exists {
				return "", errors.New("duplicate managed configuration key")
			}
			if err := Validate(map[string]int64{key: value}, false); err != nil {
				return "", err
			}
			values[key] = value
		}
	}
	for key, value := range changes {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result strings.Builder
	result.WriteString(prefix)
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		result.WriteByte('\n')
	}
	result.WriteString(blockStart + "\n")
	for _, key := range keys {
		fmt.Fprintf(&result, "%s: %d\n", key, values[key])
	}
	result.WriteString(blockEnd + "\n")
	return result.String(), nil
}

type File struct{ Path string }

func (f File) Read() (string, error) {
	raw, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(raw), err
}

// Publish replaces the file in its own directory and syncs both file and parent.
// A sync failure after rename is an uncertain publication, never automatic rollback.
func (f File) Publish(content string) error {
	mode := os.FileMode(0640)
	if info, err := os.Lstat(f.Path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("configuration target is not a regular file")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(f.Path)
	temporary, err := os.CreateTemp(dir, ".happyro-settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err = temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err = temporary.WriteString(content); err != nil {
		return err
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if err = os.Rename(temporary.Name(), f.Path); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

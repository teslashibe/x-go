package x

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestFixturesAreSanitized guards this public repository: recorded GraphQL
// fixtures carry only remapped IDs, u<n> handles and placeholder URLs, and
// the sanitizer carries no post IDs or job UUIDs (sanitize.py's own check
// also compares every output against the original values).
func TestFixturesAreSanitized(t *testing.T) {
	var (
		remapped    = regexp.MustCompile(`^10+[1-9][0-9]{0,3}$`)
		syntheticID = regexp.MustCompile(`^(79999999999999999[0-9]{2}|9223372036854775807)$`)
		digitRun    = regexp.MustCompile(`[0-9]{9,}`)
		handle      = regexp.MustCompile(`^u[0-9]+$`)
		placeholder = regexp.MustCompile(`^https://(example\.com/s[0-9]+|pbs\.twimg\.com/media/sanitized[0-9]+\.jpg)$`)
		uuid        = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	)
	files, err := filepath.Glob(filepath.Join("testdata", "graphql", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures: %v %v", files, err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var body any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		var walk func(key string, v any)
		walk = func(key string, v any) {
			switch v := v.(type) {
			case map[string]any:
				for k, item := range v {
					walk(k, item)
				}
			case []any:
				for _, item := range v {
					walk(key, item)
				}
			case string:
				switch {
				case key == "screen_name" || key == "in_reply_to_screen_name":
					if !handle.MatchString(v) {
						t.Errorf("%s: %s %q is not a u<n> handle", file, key, v)
					}
				case strings.HasPrefix(v, "http"):
					if !placeholder.MatchString(v) {
						t.Errorf("%s: %s is not a placeholder URL", file, key)
					}
				case strings.HasSuffix(key, "_msec") || strings.HasSuffix(key, "_msecs"):
					// Timestamps, shifted by sanitize.py.
				default:
					for _, run := range digitRun.FindAllString(v, -1) {
						if !remapped.MatchString(run) && !syntheticID.MatchString(run) {
							t.Errorf("%s: %s holds an ID that is not remapped", file, key)
						}
					}
				}
				if uuid.MatchString(v) {
					t.Errorf("%s: %s holds a UUID", file, key)
				}
			}
		}
		walk("", body)
	}

	script, err := os.ReadFile(filepath.Join("testdata", "graphql", "sanitize.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range regexp.MustCompile(`[0-9]{12,}`).FindAllString(string(script), -1) {
		if !syntheticID.MatchString(run) {
			t.Errorf("sanitize.py holds a literal ID %s…", run[:4])
		}
	}
	if uuid.Match(script) {
		t.Error("sanitize.py holds a UUID")
	}
}

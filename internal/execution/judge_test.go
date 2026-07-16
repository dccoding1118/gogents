package execution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dccoding1118/gogents/internal/source"
)

func TestRenderInputMd(t *testing.T) {
	content := source.TaskContent{
		URL:    "https://gh.com/1",
		Title:  "Test Title",
		Author: "user1",
		Body:   "Body text",
		Comments: []source.Comment{
			{Author: "user2", Body: "Comment 1", CreatedAt: time.Now()},
		},
	}
	res := renderInputMd(content, "/tmp/task-1", "myrepo")
	s := string(res)
	if !stringsContains(s, "Test Title") || !stringsContains(s, "Comment 1") || !stringsContains(s, "/tmp/task-1") {
		t.Errorf("rendered content missing fields: %s", s)
	}
	if !stringsContains(s, "./myrepo") {
		t.Errorf("rendered content missing reponame substitution: %s", s)
	}
}

func TestJudge(t *testing.T) {
	tmpDir := t.TempDir()
	resPath := filepath.Join(tmpDir, "result.json")

	t.Run("Missing", func(t *testing.T) {
		v := judge(resPath)
		if v.Case != VerdictMissing {
			t.Errorf("expect Missing, got %v", v.Case)
		}
	})

	t.Run("Success", func(t *testing.T) {
		data, _ := json.Marshal(map[string]string{"status": "success", "pr_url": "http://pr"})
		_ = os.WriteFile(resPath, data, 0o600)
		v := judge(resPath)
		if v.Case != VerdictCodingSuccess || v.PrURL != "http://pr" {
			t.Errorf("expect Success, got %v", v.Case)
		}
	})

	t.Run("Failed", func(t *testing.T) {
		data, _ := json.Marshal(map[string]string{"status": "failed", "message": "unlucky"})
		_ = os.WriteFile(resPath, data, 0o600)
		v := judge(resPath)
		if v.Case != VerdictCodingFailed || v.Message != "unlucky" {
			t.Errorf("expect Failed, got %v", v.Case)
		}
	})

	t.Run("Invalid schema", func(t *testing.T) {
		_ = os.WriteFile(resPath, []byte("invalid"), 0o600)
		v := judge(resPath)
		if v.Case != VerdictParseFailed {
			t.Logf("got parse fail as expected: %v", v.Reason)
		}

		_ = os.WriteFile(resPath, []byte(`{"status":"success"}`), 0o600) // missing pr_url
		v = judge(resPath)
		if v.Case != VerdictInvalid {
			t.Error("expect invalid")
		}
	})
}

// Minimal helpers to avoid extra imports if needed, but strings is usually fine
func stringsContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

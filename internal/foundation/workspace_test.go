package foundation

import (
	"path/filepath"
	"testing"
)

func TestWorkspacePaths(t *testing.T) {
	tmpDir := t.TempDir()
	absDir, _ := filepath.Abs(tmpDir)
	p := NewWorkspacePaths(absDir)

	t.Run("Hash", func(t *testing.T) {
		if len(p.Hash()) != 8 {
			t.Error("hash len")
		}
	})

	t.Run("Init", func(t *testing.T) {
		if err := p.Init(); err != nil {
			t.Error(err)
		}
	})

	t.Run("Getters", func(t *testing.T) {
		p.Root()
		p.Gogents()
		p.RuntimeYaml()
		p.StateYaml()
		p.TasksDir()
		p.TaskDir("1")
		p.TaskYaml("1")
		p.InputMd("1")
		p.OutputJsonl("1")
		p.ResultJson("1")
		p.GlobalConfigYaml()
		p.LogsDir()
		p.DaemonLog()
		p.ErrorLog()
	})
}

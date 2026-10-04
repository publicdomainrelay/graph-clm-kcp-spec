package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/deploy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"
)

type Record struct {
	Repo string `json:"repo"`

	Repository string `json:"repository"`

	Workspace string `json:"workspace"`

	Namespace string `json:"namespace"`

	AdminKubeconfig string `json:"adminKubeconfig"`

	WorkspaceKubeconfig string `json:"workspaceKubeconfig"`

	KcpRoot string `json:"kcpRoot"`

	KcpPort int `json:"kcpPort"`

	KinePort int `json:"kinePort"`

	SpecdPid int `json:"specdPid,omitempty"`

	SpecdLog string `json:"specdLog,omitempty"`

	ClmMod string `json:"clmMod,omitempty"`

	ClmDocDir string `json:"clmDocDir"`
}

func Dir() string {
	return filepath.Join(statedir.Dir(), "sessions")
}

func KcpRoot() string {
	return filepath.Join(statedir.Dir(), "kcp")
}

func DeployDir() string {
	return filepath.Join(statedir.Dir(), "deploy")
}

func TopLevel(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("session: %s is not inside a git repository", dir)
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return "", err
	}
	return top, nil
}

func Path(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return filepath.Join(Dir(), filepath.Base(repo)+"-"+hex.EncodeToString(sum[:])[:8]+".json")
}

func Load(repo string) (Record, bool, error) {
	data, err := os.ReadFile(Path(repo))
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	record := Record{}
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, false, fmt.Errorf("session: read %s: %w", Path(repo), err)
	}
	return record, true, nil
}

func ForDir(dir string) (Record, bool) {
	top, err := TopLevel(dir)
	if err != nil {
		return Record{}, false
	}
	record, ok, err := Load(top)
	if err != nil || !ok {
		return Record{}, false
	}
	return record, true
}

func Save(record Record) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(record.Repo), append(data, '\n'), 0o600)
}

func Remove(repo string) error {
	err := os.Remove(Path(repo))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func ExtractDeploy() (string, error) {
	target := DeployDir()
	err := fs.WalkDir(deploy.Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		destination := filepath.Join(target, path)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := deploy.Files.ReadFile(path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(path, ".sh") {
			mode = 0o755
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destination, data, mode)
	})
	return target, err
}

func Alive(pid int, want string) bool {
	if pid <= 0 {
		return false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), want)
}

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
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/deploy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"
)

type Record struct {
	Repo string `json:"repo"`

	Repository string `json:"repository"`

	Branch string `json:"branch,omitempty"`

	Workspace string `json:"workspace"`

	Namespace string `json:"namespace"`

	AdminKubeconfig string `json:"adminKubeconfig"`

	WorkspaceKubeconfig string `json:"workspaceKubeconfig"`

	KcpRoot string `json:"kcpRoot"`

	KcpPort int `json:"kcpPort"`

	KcpURL string `json:"kcpURL"`

	KcpPid int `json:"kcpPid"`

	KinePort int `json:"kinePort"`

	KineURL string `json:"kineURL"`

	KinePid int `json:"kinePid"`

	SpecdPid int `json:"specdPid,omitempty"`

	SpecdLog string `json:"specdLog,omitempty"`

	ClmMod string `json:"clmMod,omitempty"`

	ClmDocDir string `json:"clmDocDir"`
}

func Dir() string {
	return filepath.Join(statedir.Dir(), "sessions")
}

func checkoutName(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return filepath.Base(repo) + "-" + hex.EncodeToString(sum[:])[:8]
}

func RepoDir(repo string) string {
	return filepath.Join(statedir.Dir(), "repos", checkoutName(repo))
}

func BranchSlug(branch string) string {
	slug := oabranch.Slug(branch)
	if slug == "" {
		return "default"
	}
	return slug
}

func BranchDir(repo, branch string) string {
	return filepath.Join(RepoDir(repo), BranchSlug(branch))
}

func KcpRoot(repo, branch string) string {
	return filepath.Join(BranchDir(repo, branch), "kcp")
}

func deployParentDir() string {
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

func CurrentBranch(repo string) (string, error) {
	out, err := exec.Command("git", "-C", repo, "symbolic-ref", "--quiet", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("session: %s has a detached HEAD; check out the branch to work on first", repo)
	}
	return strings.TrimSpace(string(out)), nil
}

func DefaultBranch(repo string) string {
	if out, err := exec.Command("git", "-C", repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
		if name := strings.TrimPrefix(strings.TrimSpace(string(out)), "origin/"); name != "" {
			return name
		}
	}
	for _, candidate := range []string{"main", "master"} {
		if exec.Command("git", "-C", repo, "rev-parse", "-q", "--verify", "refs/heads/"+candidate).Run() == nil {
			return candidate
		}
	}
	return "main"
}

func Path(repo, branch string) string {
	return filepath.Join(Dir(), checkoutName(repo), BranchSlug(branch)+".json")
}

func LegacyPath(repo string) string {
	return filepath.Join(Dir(), checkoutName(repo)+".json")
}

func Load(repo, branch, defaultBranch string) (Record, bool, error) {
	record, ok, err := loadFile(Path(repo, branch))
	if err != nil || ok {
		return record, ok, err
	}
	legacy, ok, err := loadFile(LegacyPath(repo))
	if err != nil || !ok {
		return Record{}, false, err
	}
	if legacy.Branch != "" {
		if legacy.Branch != branch {
			return Record{}, false, nil
		}
	} else if branch != defaultBranch {
		return Record{}, false, nil
	}
	legacy.Branch = branch
	if err := Save(legacy); err != nil {
		return Record{}, false, err
	}
	if err := os.Remove(LegacyPath(repo)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Record{}, false, err
	}
	return legacy, true, nil
}

func loadFile(path string) (Record, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	record := Record{}
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, false, fmt.Errorf("session: read %s: %w", path, err)
	}
	return record, true, nil
}

func ForDir(dir string) (Record, bool) {
	top, err := TopLevel(dir)
	if err != nil {
		return Record{}, false
	}
	branch, err := CurrentBranch(top)
	if err != nil {
		return Record{}, false
	}
	record, ok, err := Load(top, branch, DefaultBranch(top))
	if err != nil || !ok {
		return Record{}, false
	}
	return record, true
}

func List() ([]Record, error) {
	entries, err := os.ReadDir(Dir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	records := []Record{}
	for _, entry := range entries {
		paths := []string{}
		if entry.IsDir() {
			children, err := os.ReadDir(filepath.Join(Dir(), entry.Name()))
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				if !child.IsDir() && strings.HasSuffix(child.Name(), ".json") {
					paths = append(paths, filepath.Join(Dir(), entry.Name(), child.Name()))
				}
			}
		} else if strings.HasSuffix(entry.Name(), ".json") {
			paths = append(paths, filepath.Join(Dir(), entry.Name()))
		}
		for _, path := range paths {
			record, ok, err := loadFile(path)
			if err != nil {
				return nil, err
			}
			if ok {
				records = append(records, record)
			}
		}
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].Repo != records[right].Repo {
			return records[left].Repo < records[right].Repo
		}
		return records[left].Branch < records[right].Branch
	})
	return records, nil
}

func Save(record Record) error {
	path := LegacyPath(record.Repo)
	if record.Branch != "" {
		path = Path(record.Repo, record.Branch)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func Remove(repo, branch string) error {
	path := LegacyPath(repo)
	if branch != "" {
		path = Path(repo, branch)
	}
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if branch != "" {
		_ = os.Remove(filepath.Dir(path))
	}
	return nil
}

func deployContentHash(fsys fs.FS) (string, error) {
	sum := sha256.New()
	err := fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", path, len(data))
		sum.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil))[:12], nil
}

func deployFileMode(path string) os.FileMode {
	if strings.HasSuffix(path, ".sh") {
		return 0o755
	}
	return 0o644
}

func deployDirComplete(fsys fs.FS, dir string) bool {
	err := fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		destination := filepath.Join(dir, path)
		if entry.IsDir() {
			info, err := os.Stat(destination)
			if err != nil || !info.IsDir() {
				return fs.ErrNotExist
			}
			return nil
		}
		source, err := fs.Stat(fsys, path)
		if err != nil {
			return err
		}
		info, err := os.Stat(destination)
		if err != nil {
			return err
		}
		if info.IsDir() || info.Size() != source.Size() {
			return fs.ErrNotExist
		}
		return nil
	})
	return err == nil
}

func writeDeployDir(fsys fs.FS, target string) error {
	return fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		destination := filepath.Join(target, path)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destination, data, deployFileMode(path))
	})
}

func extractDeployFS(fsys fs.FS) (string, error) {
	hash, err := deployContentHash(fsys)
	if err != nil {
		return "", err
	}
	parent := deployParentDir()
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(parent, hash)
	if deployDirComplete(fsys, target) {
		return target, nil
	}
	temp, err := os.MkdirTemp(parent, "."+hash+"-")
	if err != nil {
		return "", err
	}
	if err := writeDeployDir(fsys, temp); err != nil {
		os.RemoveAll(temp)
		return "", err
	}
	if err := os.Rename(temp, target); err != nil {
		os.RemoveAll(temp)
		if deployDirComplete(fsys, target) {
			return target, nil
		}
		return "", fmt.Errorf("session: publish deploy %s: %w", hash, err)
	}
	return target, nil
}

func ExtractDeploy() (string, error) {
	return extractDeployFS(deploy.Files)
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

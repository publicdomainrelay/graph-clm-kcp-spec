package piagent

import (
	"bufio"
	"maps"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
)

const (
	DefaultCommand = "npx"

	Package = "@earendil-works/pi-coding-agent@1.0.0"

	DefaultProvider = "deepseek"

	DefaultModel = "deepseek-flash"

	EnvAPIKey = "DEEPSEEK_API_KEY"

	Launcher = "deepseek-claude"

	EnvExtension = "SPECD_PI_EXTENSION"
)

func DefaultArgs() []string {
	return []string{
		"--yes", Package,
		"--provider", DefaultProvider,
		"--model", DefaultModel,
		"--no-session",
		"-ne",
		"-p",
	}
}

func APIKeyFromLauncher() string {
	path, err := exec.LookPath(Launcher)
	if err != nil {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, value, found := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !found || strings.TrimPrefix(name, "export ") != "ANTHROPIC_API_KEY" {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"'`)
	}
	if err := scanner.Err(); err != nil {
		return ""
	}
	return ""
}

type Options struct {
	Command string

	Args []string

	Dir string

	Timeout time.Duration

	Env map[string]string

	Extension string
}

func New(options Options) *claudecli.Agent {
	command := options.Command
	if command == "" {
		command = DefaultCommand
	}
	args := options.Args
	if len(args) == 0 {
		args = DefaultArgs()
	}
	env := map[string]string{}
	maps.Copy(env, options.Env)
	if options.Extension != "" {
		env[EnvExtension] = options.Extension
	}
	if _, given := env[EnvAPIKey]; !given && os.Getenv(EnvAPIKey) == "" {
		if key := APIKeyFromLauncher(); key != "" {
			env[EnvAPIKey] = key
		}
	}
	return claudecli.New(claudecli.Options{
		Command: command,
		Args:    args,
		Dir:     options.Dir,
		Timeout: options.Timeout,
		Env:     env,
	})
}

package extensions

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/go-plugin"

	"github.com/goppydae/gollm/internal/agent"
)

// Loader discovers and loads extensions (executable binaries and scripts).
type Loader struct {
	Dirs       []string
	PythonPath string
	Clients    []*plugin.Client
}

// NewLoader creates a new extension loader.
func NewLoader(dirs []string, pythonPath string) *Loader {
	return &Loader{
		Dirs:       dirs,
		PythonPath: pythonPath,
	}
}

// Load discovers extensions, starts them as subprocesses, and returns gRPC client interfaces.
func (l *Loader) Load() ([]agent.Extension, error) {
	var exts []agent.Extension

	for _, path := range l.Dirs {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			log.Printf("error stating path %s: %v", path, err)
			continue
		}

		if !info.IsDir() {
			// Single file
			if filepath.Ext(path) == ".md" {
				continue
			}
			ext, err := l.launchExtension(path)
			if err != nil {
				log.Printf("failed to load extension %s: %v", path, err)
				continue
			}
			exts = append(exts, ext)
			continue
		}

		// Directory
		entries, err := os.ReadDir(path)
		if err != nil {
			log.Printf("error reading extension directory %s: %v", path, err)
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			subPath := filepath.Join(path, entry.Name())
			
			// We skip .md files here, as those are handled by the SkillLoader
			if filepath.Ext(subPath) == ".md" {
				continue
			}

			ext, err := l.launchExtension(subPath)
			if err != nil {
				log.Printf("failed to load extension %s: %v", subPath, err)
				continue
			}
			exts = append(exts, ext)
		}
	}

	return exts, nil
}

// Cleanup kills all running extension subprocesses.
func (l *Loader) Cleanup() {
	for _, c := range l.Clients {
		c.Kill()
	}
}

func (l *Loader) launchExtension(path string) (agent.Extension, error) {
	var cmd *exec.Cmd

	// If it's a python file, execute it with the configured python interpreter
	if filepath.Ext(path) == ".py" {
		cmd = exec.Command(l.PythonPath, path)
	} else {
		// Otherwise, assume it's a compiled binary executable
		cmd = exec.Command(path)
	}

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins:         PluginMap,
		Cmd:             cmd,
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolGRPC,
		},
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}

	// Request the plugin
	raw, err := rpcClient.Dispense("extension")
	if err != nil {
		client.Kill()
		return nil, err
	}

	// Keep track of the client so we can kill it later
	l.Clients = append(l.Clients, client)

	return raw.(agent.Extension), nil
}

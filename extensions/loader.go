package extensions

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

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
// Extensions that fail to load are logged and skipped; the returned error accumulates all
// failures so callers can distinguish "nothing loaded" from "everything succeeded".
func (l *Loader) Load() ([]agent.Extension, []error) {
	var exts []agent.Extension
	var errs []error

	for _, path := range l.Dirs {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			log.Printf("error stating extension path %s: %v", path, err)
			errs = append(errs, fmt.Errorf("stat %s: %w", path, err))
			continue
		}

		if !info.IsDir() {
			if filepath.Ext(path) == ".md" {
				continue
			}
			ext, err := l.launchExtension(path)
			if err != nil {
				log.Printf("failed to load extension %s: %v", path, err)
				errs = append(errs, fmt.Errorf("load %s: %w", path, err))
				continue
			}
			exts = append(exts, ext)
			continue
		}

		// Directory — read and sort entries for deterministic load order.
		entries, err := os.ReadDir(path)
		if err != nil {
			log.Printf("error reading extension directory %s: %v", path, err)
			errs = append(errs, fmt.Errorf("readdir %s: %w", path, err))
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			subPath := filepath.Join(path, entry.Name())

			// .md files are handled by the SkillLoader, not here.
			if filepath.Ext(subPath) == ".md" {
				continue
			}

			ext, err := l.launchExtension(subPath)
			if err != nil {
				log.Printf("failed to load extension %s: %v", subPath, err)
				errs = append(errs, fmt.Errorf("load %s: %w", subPath, err))
				continue
			}
			exts = append(exts, ext)
		}
	}

	return exts, errs
}

// LoadOrLog calls Load and logs any errors, returning only the successfully loaded extensions.
func (l *Loader) LoadOrLog() []agent.Extension {
	exts, errs := l.Load()
	for _, err := range errs {
		log.Printf("extension load error: %v", err)
	}
	return exts
}

// LoadErrors joins all errors from a Load call into a single error, or nil if there were none.
func LoadErrors(errs []error) error {
	return errors.Join(errs...)
}

// Cleanup kills all running extension subprocesses.
func (l *Loader) Cleanup() {
	for _, c := range l.Clients {
		c.Kill()
	}
}

func (l *Loader) launchExtension(path string) (agent.Extension, error) {
	var cmd *exec.Cmd

	if filepath.Ext(path) == ".py" {
		cmd = exec.Command(l.PythonPath, path) // #nosec G204 — path is a discovered file, PythonPath is user config
	} else {
		cmd = exec.Command(path) // #nosec G204 — path is a discovered file from configured extension dirs
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

	raw, err := rpcClient.Dispense("extension")
	if err != nil {
		client.Kill()
		return nil, err
	}

	// Track the client so we can kill it on Cleanup.
	l.Clients = append(l.Clients, client)

	return raw.(agent.Extension), nil
}

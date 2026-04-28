package extensions

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	proto "github.com/goppydae/gollm/extensions/gen"
	"github.com/goppydae/gollm/internal/agent"
)

// extProc tracks a running extension subprocess and its gRPC connection.
type extProc struct {
	cmd        *exec.Cmd
	conn       *grpc.ClientConn
	socketPath string
}

// Loader discovers and loads extensions (executable binaries and scripts).
type Loader struct {
	Dirs       []string
	PythonPath string
	procs      []*extProc
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

// Cleanup kills all running extension subprocesses and removes their socket files.
func (l *Loader) Cleanup() {
	for _, p := range l.procs {
		_ = p.conn.Close()
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
		_ = os.Remove(p.socketPath)
	}
}

func (l *Loader) launchExtension(path string) (agent.Extension, error) {
	socketPath := filepath.Join(os.TempDir(),
		fmt.Sprintf("gollm-ext-%d-%d.sock", os.Getpid(), len(l.procs)))

	var cmd *exec.Cmd
	if filepath.Ext(path) == ".py" {
		cmd = exec.Command(l.PythonPath, path) // #nosec G204 — path is a discovered file, PythonPath is user config
	} else {
		cmd = exec.Command(path) // #nosec G204 — path is a discovered file from configured extension dirs
	}
	cmd.Env = append(os.Environ(), "GOLLM_SOCKET_PATH="+socketPath)
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", path, err)
	}

	if err := waitForSocket(socketPath, 10*time.Second); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("extension %s did not become ready: %w", path, err)
	}

	conn, err := grpc.NewClient("unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("dial %s: %w", socketPath, err)
	}

	l.procs = append(l.procs, &extProc{cmd: cmd, conn: conn, socketPath: socketPath})
	return &GRPCClient{client: proto.NewExtensionClient(conn)}, nil
}

func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for socket %s after %s", path, timeout)
}

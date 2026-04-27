//go:build mage

// Package main is the Mage build file for gollm.
// Usage: mage <target>
package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Build the gollm binary.
func Build() error {
	v := getVersion()
	ldflags := fmt.Sprintf("-X main.version=%s", v)
	return execCmd("go", "build", "-ldflags", ldflags, "-o", binaryPath(), "./cmd/glm")
}

// Release builds cross-platform binaries and archives them.
func Release() error {
	v := getVersion()
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	fmt.Printf("🚀 Releasing %s...\n", v)

	platforms := []struct {
		os   string
		arch string
	}{
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
		{"windows", "amd64"},
	}

	os.RemoveAll("dist")
	os.MkdirAll("dist", 0755)

	ldflags := fmt.Sprintf("-X main.version=%s", v)

	for _, p := range platforms {
		ext := ""
		if p.os == "windows" {
			ext = ".exe"
		}
		name := fmt.Sprintf("glm-%s-%s-%s%s", v, p.os, p.arch, ext)
		target := filepath.Join("dist", name)

		fmt.Printf("Building %s/%s...\n", p.os, p.arch)
		cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", target, "./cmd/glm")
		cmd.Env = append(os.Environ(), "GOOS="+p.os, "GOARCH="+p.arch, "CGO_ENABLED=0")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to build %s/%s: %w", p.os, p.arch, err)
		}

		// Create archive
		if p.os == "windows" {
			// .zip
			zipName := fmt.Sprintf("glm-%s-%s-%s.zip", v, p.os, p.arch)
			if err := execCmd("zip", "-j", filepath.Join("dist", zipName), target); err != nil {
				return fmt.Errorf("failed to zip %s (is zip installed?): %w", zipName, err)
			}
		} else {
			// .tar.gz
			tarName := fmt.Sprintf("glm-%s-%s-%s.tar.gz", v, p.os, p.arch)
			if err := execCmd("tar", "-czf", filepath.Join("dist", tarName), "-C", "dist", name); err != nil {
				return fmt.Errorf("failed to tar %s: %w", tarName, err)
			}
		}
	}

	if err := writeSHA256Sums("dist"); err != nil {
		return fmt.Errorf("failed to write checksums: %w", err)
	}
	fmt.Println("✅ Release artifacts written to dist/")
	return nil
}

// writeSHA256Sums generates a SHA256SUMS file for all files in dir.
func writeSHA256Sums(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	for _, e := range entries {
		if e.IsDir() || e.Name() == "SHA256SUMS" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		h := sha256.New()
		fh, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err = io.Copy(h, fh); err != nil {
			_ = fh.Close()
			return err
		}
		_ = fh.Close()
		fmt.Fprintf(f, "%x  %s\n", h.Sum(nil), e.Name())
	}
	return nil
}

func getVersion() string {
	if v := os.Getenv("VERSION"); v != "" {
		return v
	}
	if v := os.Getenv("GITHUB_REF_NAME"); v != "" {
		return v
	}
	data, err := os.ReadFile("VERSION")
	if err != nil {
		return "dev"
	}
	return strings.TrimSpace(string(data))
}

// Run tests with coverage.
func Test() error {
	args := []string{"test", "-v", "./..."}
	if os.Getenv("COVERAGE") != "" {
		args = append([]string{"test", "-coverprofile=coverage.out", "-v", "./..."})
	}
	return execCmd("go", args...)
}

// Vet checks for static analysis issues.
func Vet() error {
	return execCmd("go", "vet", "./...")
}

// Lint runs golangci-lint.
func Lint() error {
	return execCmd("golangci-lint", "run", "./...")
}

// Clean removes build artifacts.
func Clean() error {
	os.Remove("glm")
	os.Remove("coverage.out")
	return nil
}

// Format runs gofmt.
func Format() error {
	return execCmd("go", "fmt", "./...")
}

// Tidy runs go mod tidy.
func Tidy() error {
	return execCmd("go", "mod", "tidy")
}

// Generate runs protoc to generate Go gRPC stubs for extensions.
// Generate runs buf generate for all protobuf stubs.
// - buf.gen.yaml          → internal/gen/gollm/v1/ (agent service)
// - buf.gen.extensions.yaml → extensions/gen/      (plugin extension service)
func Generate() error {
	if err := execCmd("buf", "generate", "proto", "--template", "buf.gen.yaml"); err != nil {
		return err
	}
	return execCmd("buf", "generate", "extensions/proto", "--template", "buf.gen.extensions.yaml")
}

// Vuln runs govulncheck to scan for known vulnerabilities in dependencies.
func Vuln() error {
	return execCmd("go", "run", "golang.org/x/vuln/cmd/govulncheck@latest", "./...")
}

// All runs build, test, vet, lint, and vulnerability scan.
func All() error {
	if err := Build(); err != nil {
		return err
	}
	if err := Test(); err != nil {
		return err
	}
	if err := Vet(); err != nil {
		return err
	}
	if err := Lint(); err != nil {
		return err
	}
	if err := Vuln(); err != nil {
		return err
	}
	fmt.Println("✅ All checks passed")
	return nil
}

// Install builds and copies to GOPATH/bin.
func Install() error {
	return execCmd("go", "install", "./cmd/glm")
}

// Run builds and executes gollm with the given arguments.
func Run() error {
	if err := Build(); err != nil {
		return err
	}
	cmd := exec.Command(binaryPath())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func binaryPath() string {
	if runtime.GOOS == "windows" {
		return ".\\glm.exe"
	}
	return "./glm"
}

func execCmd(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

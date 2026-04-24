//go:build mage

// Package main is the Mage build file for gollm.
// Usage: mage <target>
package main

import (
	"fmt"
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
	return run("go", "build", "-ldflags", ldflags, "-o", binaryPath(), "./cmd/glm")
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
			if err := run("zip", "-j", filepath.Join("dist", zipName), target); err != nil {
				fmt.Printf("Warning: failed to zip %s (is zip installed?): %v\n", zipName, err)
			}
		} else {
			// .tar.gz
			tarName := fmt.Sprintf("glm-%s-%s-%s.tar.gz", v, p.os, p.arch)
			if err := run("tar", "-czf", filepath.Join("dist", tarName), "-C", "dist", name); err != nil {
				fmt.Printf("Warning: failed to tar %s: %v\n", tarName, err)
			}
		}
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
	return run("go", args...)
}

// Vet checks for static analysis issues.
func Vet() error {
	return run("go", "vet", "./...")
}

// Lint runs golangci-lint.
func Lint() error {
	return run("golangci-lint", "run", "./...")
}

// Clean removes build artifacts.
func Clean() error {
	os.Remove("glm")
	os.Remove("coverage.out")
	return nil
}

// Format runs gofmt.
func Format() error {
	return run("go", "fmt", "./...")
}

// Tidy runs go mod tidy.
func Tidy() error {
	return run("go", "mod", "tidy")
}

// Generate runs protoc to generate Go gRPC stubs for extensions.
func Generate() error {
	return run("protoc",
		"--go_out=.",
		"--go_opt=paths=source_relative",
		"--go-grpc_out=.",
		"--go-grpc_opt=paths=source_relative",
		"extensions/proto/extension.proto",
	)
}

// All runs build, test, vet, and lint.
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
	fmt.Println("✅ all checks passed")
	return nil
}

// Install builds and copies to GOPATH/bin.
func Install() error {
	return run("go", "install", "./cmd/glm")
}

// Run builds and executes gollm with the given arguments.
func Run(args ...string) error {
	if err := Build(); err != nil {
		return err
	}
	cmd := exec.Command(binaryPath(), args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func binaryPath() string {
	if runtime.GOOS == "windows" {
		return "glm.exe"
	}
	return "glm"
}

func run(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

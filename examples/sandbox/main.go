// Command gollm-sandbox is a standalone gRPC extension that restricts gollm
// file-system tools to the directory it is started in.
//
// Build:
//
//	cd examples/sandbox && go build -o gollm-sandbox .
//
// Use:
//
//	glm --extension /path/to/gollm-sandbox "What files are here?"
package main

import (
	"os"

	"github.com/goppydae/gollm/extensions"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	ext, err := newSandbox(root)
	if err != nil {
		panic(err)
	}
	extensions.Serve(ext)
}

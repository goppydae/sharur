{
  description = "gollm — Go agent development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs =
    { self, nixpkgs, ... }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-darwin"
        "x86_64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      devShells = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go
              gopls
              golangci-lint
              mage
              delve
              bash-completion
              imgcat
              chafa
              buf
            ];

            shellHook = ''
              export GOPATH="${"$HOME"}/go"
              export GOMODCACHE="${"$HOME"}/go/pkg/mod"
              export GOPROXY="https://proxy.golang.org,direct"
              # Isolate the build cache from any system Go toolchain so that
              # coverage compilation does not pick up stdlib artifacts compiled
              # by a different Go version (e.g. the distro Go on CI runners).
              export GOCACHE="${"$HOME"}/.cache/go-build-nix"
            '';
          };
        }
      );
    };
}

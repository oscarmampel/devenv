{
  description = "Python development shell";

  inputs = {
    container-devshell.url = "github:oscarmampel/devenv";
    flake-parts.follows = "container-devshell/flake-parts";
    nixpkgs.follows = "container-devshell/nixpkgs";
    nixpkgs-python.url = "github:cachix/nixpkgs-python";
  };

  outputs =
    inputs@{ container-devshell, flake-parts, nixpkgs-python, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      imports = [ container-devshell.flakeModules.containerDevShell ];
      systems = [ "x86_64-linux" "aarch64-linux" ];

      perSystem =
        { pkgs, system, ... }:
        let
          pythonVersion = pkgs.lib.strings.trim (builtins.readFile ./.python-version);
        in
        {
          devshell.packages = [ nixpkgs-python.packages.${system}.${pythonVersion} ];
        };
    };
}

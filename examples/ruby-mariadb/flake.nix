{
  description = "Standalone Ruby and MariaDB container-devshell example";
  inputs = {
    container-devshell.url = "path:../..";
    nixpkgs.follows = "container-devshell/nixpkgs";
    flake-parts.follows = "container-devshell/flake-parts";
  };
  outputs =
    inputs@{ container-devshell, flake-parts, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      imports = [ container-devshell.flakeModules.containerDevShell ];
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      perSystem = { config, pkgs, ... }: {
        imports = [ ./develop.nix ];
        devshell.packages = [ pkgs.curl ];
        devshell.container.environment.RAILS_ENV = "development";
        packages = {
          default = config.devShells.default.launcher;
          launcher = config.devShells.default.launcher;
        };
        apps.default = {
          type = "app";
          program = "${config.devShells.default.launcher}/bin/container-devshell";
        };
      };
    };
}

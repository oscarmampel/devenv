{
  description = "Container devshell environment integration fixture";
  inputs = {
    container-devshell.url = "path:../..";
    nixpkgs.follows = "container-devshell/nixpkgs";
    flake-parts.follows = "container-devshell/flake-parts";
  };
  outputs =
    inputs@{ container-devshell, flake-parts, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      imports = [ container-devshell.flakeModules.containerDevShell ];
      systems = [ "x86_64-linux" ];
      perSystem =
        { config, pkgs, ... }:
        {
          devshell.packages = [ pkgs.hello ];
          devshell.buildInputs = [ pkgs.zlib ];
          devshell.nativeBuildInputs = [ pkgs.pkg-config ];
          devshell.env = {
            PROJECT_BIN = "/workspace/bin";
            API_URL = "https://example.test/build/latest";
            LITERAL_DOLLAR = "literal$sign";
            XDG_DATA_HOME = "/workspace/.data";
            NIX_PATH = "project=/workspace";
          };
          devshell.container.environment.HOME = "/workspace";
          packages.launcher = config.devShells.default.launcher;
          apps.default = {
            type = "app";
            program = "${config.devShells.default.launcher}/bin/container-devshell";
          };
        };
    };
}

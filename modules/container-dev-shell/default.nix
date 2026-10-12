{ self, ... }:
let
  settings =
    { pkgs }:
    { lib, config, ... }:
    let
      packageList =
        description:
        lib.mkOption {
          type = lib.types.listOf lib.types.package;
          default = [ ];
          inherit description;
        };
      service = lib.types.submodule {
        freeformType = lib.types.attrsOf lib.types.anything;
        options = {
          volumes = lib.mkOption {
            type = lib.types.listOf lib.types.anything;
            default = [ ];
          };
          env_file = lib.mkOption {
            type = lib.types.listOf lib.types.anything;
            default = [ ];
          };
        };
      };
    in
    {
      options.devshell = lib.mkOption {
        default = { };
        description = "Nix environment and Compose configuration for the default container shell.";
        type = lib.types.submodule {
          options = {
            packages = packageList "Development packages added to the baseline tools.";
            inputsFrom = packageList "Derivations whose development inputs are inherited.";
            buildInputs = packageList "Build inputs for the development environment.";
            nativeBuildInputs = packageList "Native build inputs for the development environment.";
            env = lib.mkOption {
              type = lib.types.attrsOf lib.types.str;
              default = { };
              description = "Declarative environment variables for the internal shell.";
            };
            interactiveShell = lib.mkOption {
              default = { };
              description = "Executable and arguments used for each interactive session.";
              type = lib.types.submodule {
                options = {
                  package = lib.mkOption {
                    type = lib.types.package;
                    default = pkgs.bashInteractive;
                  };
                  executable = lib.mkOption {
                    type = lib.types.str;
                    default = "bash";
                  };
                  args = lib.mkOption {
                    type = lib.types.listOf lib.types.str;
                    default = [ "-i" ];
                  };
                };
              };
            };
            container = lib.mkOption {
              type = service;
              default = { };
              description = "Compose configuration for the development container.";
            };
            compose = lib.mkOption {
              type = lib.types.submodule {
                freeformType = lib.types.attrsOf lib.types.anything;
                options.services = lib.mkOption {
                  type = lib.types.attrsOf service;
                  default = { };
                  apply = services:
                    if services ? devshell then
                      throw "devshell.compose.services.devshell is reserved; configure devshell.container instead"
                    else
                      services;
                };
              };
              default = { };
              description = "Compose document for auxiliary services and top-level resources.";
            };
          };
        };
      };

      config.devshell.container =
        let
          defaults = self.lib.mkComposeDefaults {
            inherit pkgs;
            image = self.packages.${pkgs.stdenv.hostPlatform.system}.image;
          };
        in
        lib.mapAttrs (
          name: value: if name == "volumes" then lib.mkBefore value else lib.mkDefault value
        ) defaults.services.devshell;
    };
  containerDevShellModule = {
    perSystem = { pkgs, config, ... }: {
      imports = [ (settings { inherit pkgs; }) ];
      devShells.default = self.lib.mkContainerDevShell { inherit pkgs; } {
        shell = {
          inherit (config.devshell)
            packages
            inputsFrom
            buildInputs
            nativeBuildInputs
            env
            ;
        };
        inherit (config.devshell) interactiveShell;
        compose = config.devshell.compose // {
          services = config.devshell.compose.services // {
            devshell = config.devshell.container;
          };
        };
      };
    };
  };
in
{
  imports = [ containerDevShellModule ];
  flake.flakeModules.default = containerDevShellModule;
  flake.flakeModules.containerDevShell = containerDevShellModule;
}

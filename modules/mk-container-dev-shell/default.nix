{ withSystem, ... }:
let
  buildShell =
    { pkgs, packages }:
    {
      shell ? { },
      interactiveShell ? {
        package = pkgs.bashInteractive;
        executable = "bash";
        args = [ "-i" ];
      },
      compose ? { },
    }:
    let
      lib = pkgs.lib;
      shellPath = "${interactiveShell.package}/bin/${interactiveShell.executable}";
      innerShell = pkgs.mkShell (
        shell
        // {
          packages = [
            interactiveShell.package
            pkgs.coreutils
            pkgs.findutils
            pkgs.gnugrep
            pkgs.gnused
            pkgs.gawk
            pkgs.ncurses
            pkgs.cacert
          ]
          ++ (shell.packages or [ ]);
        }
      );
      image = packages.image;
      capture = packages.capture;
      services = pkgs.writeText "container-devshell-v2-services.json" (builtins.toJSON compose);
      artifactRoot = pkgs.writeText "container-devshell-v2-artifacts" (
        lib.concatStringsSep "\n" (
          map toString [
            image
            innerShell
            capture
            services
            packages.orchestrator
            packages.supervisor
            packages.compose-builder
            pkgs.nix
            pkgs.podman
            pkgs.docker-compose
            pkgs.systemd
            pkgs.glibcLocales
            pkgs.ncurses
            pkgs.cacert
          ]
        )
      );
      launcher = pkgs.writeShellScriptBin "container-devshell" ''
        export DOCKER_HOST="unix://''${XDG_RUNTIME_DIR:?XDG_RUNTIME_DIR is required}/podman/podman.sock"
        export PATH=${
          lib.makeBinPath [
            packages.orchestrator
            packages.supervisor
            packages.compose-builder
            capture
            pkgs.coreutils
            pkgs.nix
            pkgs.podman
            pkgs.docker-compose
            pkgs.systemd
          ]
        }:"''${PATH:-}"
        exec ${packages.orchestrator}/bin/orchestrator \
          --services ${services} \
          --image-archive ${image} \
          --artifact-root ${artifactRoot} \
          --inner-derivation ${builtins.unsafeDiscardOutputDependency innerShell.drvPath} \
          --shell-json ${
            lib.escapeShellArg (builtins.toJSON ([ shellPath ] ++ (interactiveShell.args or [ "-i" ])))
          } \
          "$@"
      '';
    in
    pkgs.mkShell {
      packages = [ launcher ];
      shellHook = "exec ${launcher}/bin/container-devshell";
      passthru = {
        inherit
          launcher
          image
          services
          capture
          artifactRoot
          innerShell
          ;
      };
    };
in
{
  flake.lib.mkContainerDevShell =
    {
      pkgs,
      packages ? withSystem pkgs.stdenv.hostPlatform.system ({ self', ... }: self'.packages),
    }:
    arguments: buildShell { inherit pkgs packages; } arguments;
}

{ ... }:
{
  systems = [
    "x86_64-linux"
    "aarch64-linux"
  ];

  perSystem =
    { self', config, ... }:
    let
      shell = config.devShells.default;
    in
    {
      packages = {
        default = shell.launcher;
        launcher = shell.launcher;
        innerShell = shell.innerShell;
        services = shell.services;
      };
      apps.default = {
        type = "app";
        program = "${self'.packages.launcher}/bin/container-devshell";
      };
    };
}

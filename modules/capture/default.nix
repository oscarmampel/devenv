{ ... }:
{
  perSystem = { pkgs, ... }: {
    packages.capture =
      let
        capture = pkgs.runCommand "container-devshell-v2-capture" { nativeBuildInputs = [ pkgs.go ]; } ''
          export HOME=$TMPDIR GOCACHE=$TMPDIR/go-cache
          mkdir -p $out/bin
          go build -trimpath -o $out/bin/capture-environment ${./filter.go}
        '';
        inner = pkgs.writeShellScriptBin "capture-inner-env" ''
          exec ${pkgs.coreutils}/bin/env -i \
            HOME="$HOME" USER="''${USER:-dev}" \
            NIX_PATH="nixpkgs=${pkgs.path}" \
            PATH=${
              pkgs.lib.makeBinPath [
                pkgs.nix
                pkgs.coreutils
                pkgs.bashInteractive
              ]
            } \
            CDS_NIX_DIRENV=${pkgs.nix-direnv}/share/nix-direnv/direnvrc \
            ${pkgs.bash}/bin/bash ${./capture-env.sh} "$@"
        '';
      in
      pkgs.writeShellScriptBin "capture-env" ''
        exec ${capture}/bin/capture-environment --capture-command ${inner}/bin/capture-inner-env "$@"
      '';
  };
}

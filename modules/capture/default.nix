{ ... }:
{
  perSystem = { pkgs, ... }: {
    packages.capture =
      let
        filter = pkgs.runCommand "container-devshell-v2-envfilter" { nativeBuildInputs = [ pkgs.go ]; } ''
          export HOME=$TMPDIR GOCACHE=$TMPDIR/go-cache
          mkdir -p $out/bin
          go build -trimpath -o $out/bin/envfilter ${./filter.go}
        '';
      in
      pkgs.writeShellScriptBin "capture-env" ''
        exec ${pkgs.coreutils}/bin/env -i \
          HOME="$HOME" USER="''${USER:-dev}" \
          NIX_PATH="nixpkgs=${pkgs.path}" \
          PATH=${
            pkgs.lib.makeBinPath [
              pkgs.nix
              pkgs.coreutils
              pkgs.bashInteractive
              filter
            ]
          } \
          CDS_NIX_DIRENV=${pkgs.nix-direnv}/share/nix-direnv/direnvrc \
          ${pkgs.bash}/bin/bash ${./capture-env.sh} "$@"
      '';
  };
}

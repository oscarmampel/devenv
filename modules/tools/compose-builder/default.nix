{ ... }:
{
  perSystem =
    { pkgs, ... }:
    {
      packages.compose-builder = pkgs.runCommand "container-devshell-v2-compose-builder" {
        nativeBuildInputs = [ pkgs.go ];
      } ''
        export HOME="$TMPDIR" GOCACHE="$TMPDIR/go-cache" GO111MODULE=off
        mkdir -p "$out/bin"
        go build -trimpath -o "$out/bin/compose-builder" ${./main.go}
      '';
    };
}

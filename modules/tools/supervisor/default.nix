{ ... }:
{
  perSystem =
    { pkgs, ... }:
    {
      packages.supervisor = pkgs.runCommand "container-devshell-v2-supervisor" {
        nativeBuildInputs = [ pkgs.go ];
      } ''
        export HOME="$TMPDIR" GOCACHE="$TMPDIR/go-cache" GO111MODULE=off
        mkdir -p "$out/bin"
        go build -trimpath -o "$out/bin/supervisor" ${./main.go}
      '';
    };
}

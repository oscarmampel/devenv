{ ... }:
{
  perSystem = { pkgs, ... }: {
    packages.image =
      let
        identity = pkgs.runCommand "container-devshell-v2-files" { } ''
          mkdir -p $out/etc
          printf 'root:x:0:0:root:/root:/bin/sh\ndev:x:1000:1000:Development:/home/dev:/bin/sh\n' > $out/etc/passwd
          printf 'root:x:0:\ndev:x:1000:\n' > $out/etc/group
          printf 'passwd: files\ngroup: files\nhosts: files dns\n' > $out/etc/nsswitch.conf
        '';
        defaultsEnvironment = {
          HOME = "/home/dev";
          USER = "dev";
          LOGNAME = "dev";
          SHELL = "/bin/sh";
          TMPDIR = "/tmp";
          TMP = "/tmp";
          TEMP = "/tmp";
          TEMPDIR = "/tmp";
          LANG = "C.UTF-8";
          LOCALE_ARCHIVE = "${pkgs.glibcLocales}/lib/locale/locale-archive";
          TERMINFO = "${pkgs.ncurses}/share/terminfo";
          TERMINFO_DIRS = "${pkgs.ncurses}/share/terminfo";
          SSL_CERT_FILE = "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";
          NIX_SSL_CERT_FILE = "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";
        };
      in
      pkgs.dockerTools.buildLayeredImage {
        name = "container-devshell-v2";
        tag = null;
        includeStorePaths = false;
        contents = [
          identity
          pkgs.dockerTools.binSh
          pkgs.dockerTools.usrBinEnv
          pkgs.dockerTools.caCertificates
        ];
        extraCommands = ''
          mkdir -p tmp var/tmp nix/store workspace home/dev run proc sys dev
          for file in passwd group nsswitch.conf; do
            cp --remove-destination ${identity}/etc/$file etc/$file
          done
          chmod 1777 tmp var/tmp
          chmod 0700 home/dev
        '';
        fakeRootCommands = "chown 1000:1000 home/dev";
        config = {
          User = "1000:1000";
          WorkingDir = "/workspace";
          Env = pkgs.lib.mapAttrsToList (name: value: "${name}=${value}") defaultsEnvironment;
        };
      };
  };
}

{ ... }:
{
  flake.lib.mkComposeDefaults = { pkgs, image }: {
    services.devshell = {
      image = "${image.imageName}:${image.imageTag}";
      command = [
        "${pkgs.coreutils}/bin/sleep"
        "infinity"
      ];
      init = true;
      user = "1000:1000";
      userns_mode = "keep-id:uid=1000,gid=1000";
      working_dir = "/workspace";
      cap_drop = [ "ALL" ];
      security_opt = [ "no-new-privileges" ];
      volumes = [
        {
          type = "bind";
          source = "/nix/store";
          target = "/nix/store";
          read_only = true;
        }
        {
          type = "bind";
          source = ".";
          target = "/workspace";
        }
      ];
    };
  };
}

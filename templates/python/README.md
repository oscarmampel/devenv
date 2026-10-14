# Python project template

This template reads `.python-version` and selects that version from
`nixpkgs-python`. It adds no Cachix substituter or trusted key. The first build
may compile Python locally if it is not already available from your configured
caches.

Run `nix develop` from this directory. If this is a Git repository, add
`flake.nix` and `.python-version` to Git before running Nix so the flake can
read the version file.

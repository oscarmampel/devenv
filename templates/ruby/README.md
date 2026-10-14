# Ruby project template

This template reads `.ruby-version` and selects that exact Ruby version from
`nixpkgs-ruby`. MariaDB Connector/C supplies the MySQL client library for native
gems such as `mysql2`.

The `mysql` service has no published port. It shares an internal Compose
network with the development container; the development container also joins
the default network for outbound access. For local development, MySQL starts
with an empty root password. Change its authentication settings before using
this configuration with non-local data.

Run `nix develop` from this directory. If this is a Git repository, add
`flake.nix` and `.ruby-version` to Git before running Nix, so the flake can read
the version file. Manage project gems with Bundler as usual.

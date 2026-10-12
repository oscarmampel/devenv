# Container DevShell

`nix develop` opens a development shell in a rootless Podman container. Nix builds
the shell environment and a minimal image; Docker Compose v2 starts the container
and any services it depends on.

Requires Linux, Nix 2.26 or newer with flakes, rootless Podman with its user
socket at `$XDG_RUNTIME_DIR/podman/podman.sock`, and working `newuidmap` and
`newgidmap` on the host.

```sh
nix develop                                  # Interactive shell
nix run . -- -- /bin/sh -c 'id; pwd'         # Run a command
nix run . -- --cleanup                       # Clean an inactive project
```

Import `flakeModules.containerDevShell` in a flake-parts project and configure
`devshell` inside `perSystem`:

```nix
imports = [ container-devshell.flakeModules.containerDevShell ];
perSystem = { pkgs, ... }: {
  devshell.packages = [ pkgs.ruby ];
  devshell.buildInputs = [ pkgs.zlib ];
  devshell.env.APP_MODE = "development";

  devshell.container.depends_on.cache.condition = "service_started";
  devshell.compose.services.cache.image = "docker.io/library/redis:7";
};
```

`packages`, `inputsFrom`, `buildInputs`, `nativeBuildInputs`, and `env` are
passed separately to `pkgs.mkShell`. Use `buildInputs` for libraries and their
setup hooks, and `packages` for ordinary development tools. `env` values are
literal strings: `$PWD` is not expanded. The optional `interactiveShell` has
`package`, `executable`, and `args` fields; it defaults to interactive Bash.
User-defined `shellHook` is not supported.

`devshell.container` configures the main Compose service. It has defaults for
the image, a writable project mount at `/workspace`, and the `/nix/store` mount.
`devshell.compose.services` defines auxiliary services; top-level Compose
resources such as volumes and networks also stay under `devshell.compose`.
Nix inserts `devshell.container` as `services.devshell` in the generated
`services.json`, so that service name cannot be defined again in
`devshell.compose.services`. Explicit `devshell.container.environment` values
override variables captured from the Nix shell. To override `HOME` inside the
container, set `devshell.container.environment.HOME`.

Run the launcher from the project directory. Its canonical path identifies the
project, and Compose resolves relative mounts and `env_file` paths from there.
Sessions in the same directory share the container and services. Configuration
changes take effect after the last session exits; that exit runs Compose
`down` without removing named volumes. If a session exits unexpectedly, the
next entry cleans up the abandoned generation. `--cleanup` performs this step
manually when no session is active.

Runtime state is kept under `${XDG_STATE_HOME:-$HOME/.local/state}/shell-sandbox/`
and `$XDG_RUNTIME_DIR/shell-sandbox/`. The generated Compose file may contain
environment values, so it is private, stays outside the Nix store and Git, and
is removed after a successful `down`. Do not put secrets in Nix options; use
runtime files such as Compose `env_file` instead. Per-project Nix GC roots are
retained after `down`. A failed cleanup retains the generation for a later
`--cleanup` attempt.

The [Ruby/MariaDB example](examples/ruby-mariadb/README.md) is a separate flake
that imports this module. To check the implementation:

```sh
nix flake check .
nix flake check ./examples/ruby-mariadb
nix flake check ./tests/environment
nix build --no-link --print-out-paths .#launcher
bash tests/integration.sh /nix/store/<result>/bin/container-devshell
```

The integration test uses isolated projects and state under `/tmp`; it does not
remove named volumes or run global Podman cleanup. The implementation has been
tested on x86_64 Linux. aarch64, SELinux, other Podman network backends, and
Nix garbage collection during a live session remain untested.

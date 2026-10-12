# Agent instructions

Keep `nix develop` as the public entry point. Prefer small changes that let
Nix, Podman, and Docker Compose handle their own responsibilities. Preserve
unrelated user changes and do not add custom health checks, volume locking,
hot reload, or background recovery loops.

The public flake-parts module is `flakeModules.containerDevShell`. Its
per-system `devshell` option provides the Nix shell inputs (`packages`,
`inputsFrom`, `buildInputs`, `nativeBuildInputs`, `env`), `interactiveShell`,
`container` for the main Compose service, and `compose` for auxiliary services
and top-level resources. The module generates `services.json` with
`services.devshell` taken from `devshell.container`. Preserve Nix option merge
semantics and the distinction between `packages` and `buildInputs`.

Nix builds the inner and outer shells, minimal image, Go tools, and static
`services.json`. The host-side launcher runs the orchestrator. It identifies a
project from its canonical working directory, captures the Nix environment
with Bash and nix-direnv, and writes a private runtime Compose snapshot. The
supervisor manages sessions, signals, Compose `up`/`down`, and `podman exec`.
Active sessions reuse their generation; the final session runs `down`.

Keep environment values and other secrets out of the Nix store, images, logs,
and tracked files. Runtime Compose files must be private and removed after a
successful `down`. Preserve the generation if cleanup fails. Do not introduce
Python, Node, or Ruby as runtime dependencies of the helper.

For changes, run the relevant Nix checks and `git diff --check`. Run
`tests/integration.sh` with real rootless Podman when changing the launcher,
environment capture, Compose output, or session lifecycle. Coordinate tests
that share projects, ports, or volumes; do not perform global cleanup or
remove named volumes. Report what was tested and any remaining limits.

#!/usr/bin/env bash
# Run against a built launcher. Uses isolated projects and no global Podman cleanup.
set -euo pipefail

launcher=$(realpath "${1:?pass an absolute launcher path}")
environment_fixture=$(realpath "$(dirname "$0")/environment")
base=$(mktemp -d /tmp/shell-sandbox-projects.XXXXXX)
export XDG_STATE_HOME
XDG_STATE_HOME=$(mktemp -d /tmp/shell-sandbox-state.XXXXXX)
root="$base/project with spaces \${literal}"
other="$base/other/$(basename "$root")"
mkdir -p "$root" "$other"
first=

cleanup() {
  touch "$root/release" 2>/dev/null || true
  if [[ -n $first ]]; then
    kill "$first" 2>/dev/null || true
    wait "$first" 2>/dev/null || true
  fi
  (cd "$root" && "$launcher" --cleanup >/dev/null 2>&1) || true
  (cd "$other" && "$launcher" --cleanup >/dev/null 2>&1) || true
  if [[ -n ${fixture_launcher:-} ]]; then
    (cd "$environment_fixture" && "$fixture_launcher/bin/container-devshell" --cleanup >/dev/null 2>&1) || true
  fi
}
trap cleanup EXIT

run_in() {
  local directory=$1
  shift
  (cd "$directory" && "$launcher" -- "$@")
}

wait_for() {
  local path=$1
  for ((attempt = 0; attempt < 300; attempt++)); do
    [[ -e $path ]] && return 0
    sleep 0.1
  done
  printf 'Timed out waiting for %s\n' "$path" >&2
  return 1
}

(cd "$root" && exec "$launcher" -- /bin/sh -c '
  touch /workspace/ready
  while [ ! -e /workspace/release ]; do sleep 0.1; done
  test -f "$HOME/shared"
') &
first=$!
wait_for "$root/ready"
active_file=$(find "$XDG_STATE_HOME/shell-sandbox" -name active.json -print -quit)
[[ -n $active_file ]]
[[ -z $(find "$XDG_STATE_HOME/shell-sandbox" -name config.json -print -quit) ]]
active_inode=$(stat -c %i "$active_file")
artifact_link="${active_file%/*}/artifacts"
[[ -L $artifact_link ]]
run_in "$root" /bin/sh -c 'test "$PWD" = /workspace && test "$(id -u)" = 1000 && echo shared > "$HOME/shared" && touch /workspace/owned'
[[ $(stat -c %u "$root/owned") = $(id -u) ]]
[[ $(stat -c %i "$active_file") = "$active_inode" ]]
kill -0 "$first"
if (cd "$root" && "$launcher" --cleanup); then
  echo 'cleanup accepted a live session' >&2
  exit 1
fi

run_in "$other" /bin/sh -c 'test "$PWD" = /workspace && touch /workspace/other-owned'
[[ -f $other/other-owned ]]
kill -0 "$first"

touch "$root/release"
wait "$first"
first=
[[ -L $artifact_link ]]
run_in "$root" /bin/sh -c 'test ! -f "$HOME/shared"'
[[ -L $artifact_link ]]

# SIGKILL skips supervisor cleanup. A new entry must stop the old generation
# before preparing a replacement, while keeping the persistent artifact root.
(cd "$root" && exec "$launcher" -- /bin/sh -c 'touch /workspace/ready-kill; sleep 60') &
first=$!
wait_for "$root/ready-kill"
kill -KILL "$first"
wait "$first" 2>/dev/null || true
first=
run_in "$root" /bin/sh -c 'test "$PWD" = /workspace'
[[ -L $artifact_link ]]

# Manual cleanup also recognizes a dead supervisor without recapturing Nix.
(cd "$root" && exec "$launcher" -- /bin/sh -c 'touch /workspace/ready-kill-manual; sleep 60') &
first=$!
wait_for "$root/ready-kill-manual"
kill -KILL "$first"
wait "$first" 2>/dev/null || true
first=
(cd "$root" && "$launcher" --cleanup)
[[ -L $artifact_link ]]

# If removal was interrupted after active.json was unlinked, manual cleanup
# collects the private orphan without touching the persistent Nix GC root.
[[ ! -e $active_file ]]
interrupted="${active_file%/*}/candidate-interrupted"
mkdir "$interrupted"
touch "$interrupted/compose.json"
(cd "$root" && "$launcher" --cleanup)
[[ ! -e $interrupted && -L $artifact_link ]]

# The independent flake exercises declarative variables, packages, a literal
# dollar in an environment value, and isolation from the host environment.
fixture_launcher=$(nix build --no-link --print-out-paths "$environment_fixture#launcher")
(cd "$environment_fixture" && HOST_ONLY=host-only "$fixture_launcher/bin/container-devshell" -- '/workspace/bin$tools/check-environment')

printf 'Podman integration passed; state retained at %s\n' "$XDG_STATE_HOME"

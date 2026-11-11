#!/usr/bin/env bash
# Run only through capture-inner-env, which supplies a clean environment.
set -eo pipefail
project_root=
state_dir=
output=
target=
while (($#)); do
  case "$1" in
    --project-root) project_root=$2; shift 2 ;;
    --state-dir) state_dir=$2; shift 2 ;;
    --output) output=$2; shift 2 ;;
    --derivation) target=$2; shift 2 ;;
    *) printf 'capture-env: unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done
[[ -n $project_root && -n $state_dir && -n $output && -n $target ]] || exit 2
umask 077
mkdir -p "$state_dir/nix-direnv"
cd "$project_root"
export NIX_DIRENV_SKIP_VERSION_CHECK=1 direnv=container-devshell-compat
has() { command -v "$1" >/dev/null; }
log_status() { printf '%s\n' "$*" >&2; }
log_error() { log_status "$@"; }
direnv_layout_dir() { printf '%s\n' "$state_dir/nix-direnv"; }
PATH_add() { export PATH="$1:$PATH"; }
watch_file() { :; }
_cds_env_command=$(command -v env)
_cds_remove_command=$(command -v rm)
readonly _cds_env_command _cds_remove_command
_cds_capture_baseline=$(mktemp "$state_dir/.environment-before.XXXXXX")
_cds_capture_after=$(mktemp "$state_dir/.environment-after.XXXXXX")
readonly _cds_capture_baseline _cds_capture_after
trap '"$_cds_remove_command" -f -- "$_cds_capture_baseline" "$_cds_capture_after"' EXIT
"$_cds_env_command" -0 > "$_cds_capture_baseline"
source "$CDS_NIX_DIRENV"
# Its GC-root refresh uses globs for both Nix and flake profiles; only one
# kind exists in a given project. Avoid literal unmatched globs with set -e.
shopt -s nullglob
nix_direnv_disallow_fallback
invalidate_cache() {
  # Keep the GC roots until nix-direnv has built their replacement.
  local cached
  for cached in "$state_dir/nix-direnv/"*.rc; do
    [[ ! -f $cached ]] || rm -f -- "$cached"
  done
}
inner_expr=$(printf 'builtins.storePath "%s"' "$target")
if [[ ! -f $state_dir/inner.nix || $(<"$state_dir/inner.nix") != "$inner_expr" ]]; then
  printf '%s\n' "$inner_expr" > "$state_dir/inner.nix"
  invalidate_cache
fi
use_nix "$state_dir/inner.nix"
[[ ${NIX_DIRENV_DID_FALLBACK:-0} != 1 ]] || exit 1
"$_cds_env_command" -0 > "$_cds_capture_after"

# Emit only shell changes. The Go caller handles host references and paths.
declare -A before=()
while IFS= read -r -d '' entry; do
  key=${entry%%=*}
  before["$key"]=${entry#*=}
done < "$_cds_capture_baseline"
: > "$output"
while IFS= read -r -d '' entry; do
  key=${entry%%=*}
  value=${entry#*=}
  if [[ ${before[$key]+present} && ${before[$key]} == "$value" ]]; then
    continue
  fi
  case "$key" in
    _|SHLVL|direnv|CDS_*|BASH_FUNC_*|NIX_DIRENV_*|DIRENV_*) continue ;;
  esac
  printf '%s\0' "$entry" >> "$output"
done < "$_cds_capture_after"

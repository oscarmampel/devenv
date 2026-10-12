#!/usr/bin/env bash
# Run only through the Nix wrapper, which supplies a clean environment.
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
_cds_filter_command=$(command -v envfilter)
_cds_remove_command=$(command -v rm)
readonly _cds_env_command _cds_filter_command _cds_remove_command
_cds_capture_baseline=$(mktemp "$state_dir/.environment-before.XXXXXX")
readonly _cds_capture_baseline
trap '"$_cds_remove_command" -f -- "$_cds_capture_baseline"' EXIT
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
# Hooks may write to stdout; the data channel is a private file, not stdout.
export CDS_CAPTURE_OUTPUT="$output" CDS_CAPTURE_PROJECT="$project_root"
"$_cds_env_command" -0 | "$_cds_filter_command" --baseline "$_cds_capture_baseline" --project-root "$project_root" --output "$output"

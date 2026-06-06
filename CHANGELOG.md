# Changelog

## v0.2.3

- Add pixi/nebi environment activation support. When `JHUB_APP_ENV_MANAGER=pixi`,
  the proxy resolves the selected environment via `nebi workspace list` and runs
  the app inside that pixi environment instead of attempting conda activation.
- Match nebi workspaces by `origin_name` (server workspace name) when the local
  pixi workspace name differs.

## v0.2.2

- Warn and continue without activation when conda activation fails.
- Install-script resilience and POSIX compliance fixes.

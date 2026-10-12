# Ruby/MariaDB example

This is a separate flake that imports the root project. Credentials live in
local Git-ignored files and are read when the containers start, not by Nix.

```sh
mkdir -p secrets
printf 'example-password\n' > secrets/database-password
printf 'MARIADB_ROOT_PASSWORD=example-root\nMARIADB_USER=developer\nMARIADB_PASSWORD=example-password\n' > secrets/database.env
nix run . -- -- ruby -rsocket -e 'abort unless File.file?(ENV.fetch("DATABASE_PASSWORD_FILE")); Socket.getaddrinfo("database", 3306); puts "Ruby and MariaDB are reachable"'
```

`nix develop` opens an interactive shell. Compose keeps the `database-data`
volume after the session ends, including after `--cleanup`. `PROJECT_BIN` is
`/workspace/bin`; `/workspace/bin/example-path-check` checks it in the
container.

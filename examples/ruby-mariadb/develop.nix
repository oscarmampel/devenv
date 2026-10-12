{pkgs, ...}: {
  devshell = {
    packages = [
      pkgs.ruby
      pkgs.mariadb-connector-c
      pkgs.pkg-config
    ];
    env.PROJECT_BIN = "/workspace/bin";
    container = {
      environment = {
        RAILS_ENV = "development";
        DATABASE_HOST = "database";
        DATABASE_USER = "developer";
        DATABASE_PASSWORD_FILE = "/run/secrets/database-password";
      };
      volumes = [
        {
          type = "bind";
          source = "./secrets/database-password";
          target = "/run/secrets/database-password";
          read_only = true;
        }
      ];
      depends_on.database.condition = "service_healthy";
    };
    compose.services.database = {
      image = "docker.io/library/mariadb:11.4";
      environment.MARIADB_DATABASE = "development";
      env_file = ["./secrets/database.env"];
      volumes = ["database-data:/var/lib/mysql"];
      healthcheck = {
        test = [
          "CMD"
          "healthcheck.sh"
          "--connect"
          "--innodb_initialized"
        ];
        interval = "2s";
        timeout = "5s";
        retries = 30;
        start_period = "10s";
      };
    };
    compose.volumes.database-data = {};
  };
}

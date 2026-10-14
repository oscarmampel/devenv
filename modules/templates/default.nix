{ ... }:
{
  flake.templates.python = {
    path = ../../templates/python;
    description = "Python development shell using .python-version";
  };

  flake.templates.ruby = {
    path = ../../templates/ruby;
    description = "Ruby development shell with a private MySQL service";
  };
}

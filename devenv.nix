{ pkgs, ... }:

{
  packages = [ pkgs.git pkgs.just pkgs.watchexec ];

  languages.go.enable = true;
}

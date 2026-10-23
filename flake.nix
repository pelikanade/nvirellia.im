{
  description = "nvirellia.im profile sheet";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          nvirellia = pkgs.buildGoModule {
            pname = "nvirellia";
            version = "0-unstable-${self.shortRev or self.dirtyShortRev or "unknown"}";
            src = self;
            subPackages = [ "cmd/nvirellia" ];
            vendorHash = null;
            ldflags = [
              "-s"
              "-w"
            ];
            meta = {
              description = "nvirellia.im profile sheet";
              mainProgram = "nvirellia";
            };
          };
        in
        {
          default = nvirellia;
          inherit nvirellia;
        }
      );

      nixosModules.default = import ./nix/module.nix { inherit self; };

      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt);
    };
}

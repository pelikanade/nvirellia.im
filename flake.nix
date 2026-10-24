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
          nvirellia-im = pkgs.buildGoModule {
            pname = "nvirellia-im";
            version = "0-unstable-${self.shortRev or self.dirtyShortRev or "unknown"}";
            src = self;
            subPackages = [ "cmd/nvirellia-im" ];
            vendorHash = null;
            ldflags = [
              "-s"
              "-w"
            ];
            meta = {
              description = "nvirellia.im profile sheet";
              mainProgram = "nvirellia-im";
            };
          };
        in
        {
          default = nvirellia-im;
          inherit nvirellia-im;
        }
      );

      nixosModules.default = import ./nix/module.nix { inherit self; };

      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt);
    };
}

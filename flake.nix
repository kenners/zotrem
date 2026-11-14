{
  description = "Two-way sync between Zotero and reMarkable";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs =
    { nixpkgs, self }:
    let
      linuxSystems = [
        "aarch64-linux"
        "x86_64-linux"
      ];
      forSystems = systems: nixpkgs.lib.genAttrs systems;
      zotrem =
        pkgs:
        pkgs.callPackage ./nix/package.nix {
          rev = self.shortRev or self.dirtyShortRev or "unknown";
          vendorHash = "sha256-A9qfyYN/0WVlSevZn6d8EGZ0hho/wRywd5rF3fHH/cQ=";
          version = "0.1.0";
        };
    in
    {
      checks = forSystems linuxSystems (
        system:
        let
          node = nixpkgs.lib.nixosSystem {
            inherit system;
            modules = [
              self.nixosModules.default
              {
                boot.isContainer = true;
                services.zotrem.enable = true;
                system.stateVersion = "26.05";
              }
            ];
          };
        in
        {
          module = node.config.systemd.units."zotrem.service".unit;
          package = self.packages.${system}.default;
          vm = import ./nix/test.nix {
            module = self.nixosModules.default;
            pkgs = nixpkgs.legacyPackages.${system};
          };
        }
      );

      nixosModules = {
        default = self.nixosModules.zotrem;
        zotrem = import ./nix/module.nix self;
      };

      overlays.default = final: _prev: { zotrem = zotrem final; };

      packages = forSystems (linuxSystems ++ [ "aarch64-darwin" ]) (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = self.packages.${system}.zotrem;
          zotrem = zotrem pkgs;
        }
      );
    };
}

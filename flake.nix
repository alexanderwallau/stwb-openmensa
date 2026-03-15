{
  description = "An openmensa compatible webserver for the Studierendenwerk Bonn";

  inputs.nixpkgs.url = "nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      lastModifiedDate = self.lastModifiedDate or self.lastModified or "19700101";
      version = builtins.substring 0 8 lastModifiedDate;

      supportedSystems = [ "x86_64-linux" "x86_64-darwin" "aarch64-linux" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
          stwb-openmensa = pkgs.buildGoModule {
            pname = "stwb-openmensa";
            inherit version;
            src = ./.;
            # No external Go dependencies — stdlib only.
            vendorHash = null;
          };
        in
        {
          inherit stwb-openmensa;
          default = stwb-openmensa;
        });

      devShells = forAllSystems (system:
        let pkgs = nixpkgsFor.${system}; in
        {
          default = pkgs.mkShell {
            buildInputs = with pkgs; [ go gopls gotools go-tools ];
          };
        });

      # NixOS module — consume with:
      #
      #   inputs.stwb-openmensa.url = "github:alexanderwallau/stwb-openmensa";
      #
      #   # in your NixOS configuration:
      #   imports = [ inputs.stwb-openmensa.nixosModules.default ];
      #   services.stwb-openmensa = {
      #     enable       = true;
      #     package      = inputs.stwb-openmensa.packages.${pkgs.system}.default;
      #     port         = 8080;
      #     listenAddress = "127.0.0.1";
      #     refreshTimes = [ "07:00" "11:00" "14:00" "17:00" ];
      #   };
      nixosModules.default = import ./nix/module.nix;

      # Convenience alias kept for backwards compat with older flake consumers.
      defaultPackage = forAllSystems (system: self.packages.${system}.default);
    };
}

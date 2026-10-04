{
  description = "Urth: synthetic monitoring with probes that run inside the networks they test";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      inherit (nixpkgs) lib;
      # Nixpkgs unstable no longer supports x86_64-darwin; Intel Macs use the
      # Homebrew cask or the darwin_amd64 release archive.
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      names = [
        "urth-api-srv"
        "urth-worker"
        "urthctl"
      ];
      # PolyForm Noncommercial is not a free licence, so Nixpkgs refuses these
      # packages by default. This flake's own outputs accept exactly these three;
      # overlay users decide for themselves through allowUnfreePredicate.
      forAllSystems =
        f:
        lib.genAttrs systems (
          system:
          f (
            import nixpkgs {
              inherit system;
              config.allowUnfreePredicate = drv: lib.elem (lib.getName drv) names;
            }
          )
        );
      license = {
        spdxId = "PolyForm-Noncommercial-1.0.0";
        fullName = "PolyForm Noncommercial License 1.0.0";
        url = "https://polyformproject.org/licenses/noncommercial/1.0.0/";
        free = false;
        redistributable = true;
      };

      # Builds use the same inputs as the container images: Go sources only.
      # Bump vendorHash whenever go.mod or go.sum changes; the Nix workflow
      # fails with the expected value when it is stale.
      urthPackages =
        pkgs:
        let
          module =
            {
              pname,
              subPackage,
              command,
              description,
              tags ? [ ],
              platforms ? lib.platforms.unix,
            }:
            pkgs.buildGo127Module {
              inherit pname tags;
              version = self.shortRev or self.dirtyShortRev or "dev";
              src = lib.fileset.toSource {
                root = ./.;
                fileset = lib.fileset.unions [
                  ./go.mod
                  ./go.sum
                  ./cmd
                  ./pkg
                ];
              };
              vendorHash = "sha256-fLWj4i6Gq6Hb9NJtmVg7HR6GZ/zaQN1grPfUp0T459A=";
              subPackages = [ subPackage ];
              env.CGO_ENABLED = 0;
              ldflags = [
                "-s"
                "-w"
              ];
              # Package tests need PostgreSQL and NATS fixtures; CI runs them.
              doCheck = false;
              # Prefix server binaries so they cannot collide with another
              # product's api-server on the same PATH.
              postInstall = lib.optionalString (baseNameOf subPackage != command) ''
                mv $out/bin/${baseNameOf subPackage} $out/bin/${command}
              '';
              meta = {
                inherit description platforms;
                homepage = "https://github.com/sre-norns/urth";
                inherit license;
                mainProgram = command;
              };
            };
        in
        {
          urth-api-srv = module {
            pname = "urth-api-srv";
            subPackage = "cmd/api-server";
            command = "urth-api-srv";
            description = "Urth API server";
            platforms = lib.platforms.linux;
          };
          urth-worker = module {
            pname = "urth-worker";
            subPackage = "cmd/nats-worker";
            command = "urth-worker";
            description = "Urth worker that executes probes inside the network it serves";
            platforms = lib.platforms.linux;
          };
          urthctl = module {
            pname = "urthctl";
            subPackage = "cmd/urthctl";
            command = "urthctl";
            description = "Urth command-line client";
          };
        };
    in
    {
      # Server packages are Linux only; each system lists what it can build.
      packages = forAllSystems (
        pkgs:
        let
          available = lib.filterAttrs (_: lib.meta.availableOn pkgs.stdenv.hostPlatform) (urthPackages pkgs);
        in
        available // { default = available.urthctl; }
      );

      overlays.default = final: _prev: urthPackages final;

      apps = forAllSystems (
        pkgs:
        lib.mapAttrs (_: drv: {
          type = "app";
          program = lib.getExe drv;
        }) (self.packages.${pkgs.stdenv.hostPlatform.system})
      );

      checks = forAllSystems (pkgs: removeAttrs self.packages.${pkgs.stdenv.hostPlatform.system} [ "default" ]);
    };
}

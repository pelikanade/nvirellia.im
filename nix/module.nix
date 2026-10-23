# NixOS module for nvirellia.im, exported as nixosModules.default.
#
# The service runs as a dynamic user. The magpie key is supplied through an
# EnvironmentFile read by systemd before privileges drop, so it never enters
# the Nix store.
{ self }:
{
  config,
  lib,
  pkgs,
  ...
}:

let
  inherit (lib)
    mkEnableOption
    mkIf
    mkOption
    optional
    optionalAttrs
    types
    ;

  cfg = config.services.nvirellia;
in
{
  options.services.nvirellia = {
    enable = mkEnableOption "the nvirellia.im profile sheet";

    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = lib.literalExpression "inputs.nvirellia.packages.${pkgs.stdenv.hostPlatform.system}.default";
      description = "The nvirellia package to use.";
    };

    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
      example = "0.0.0.0";
      description = ''
        Address to bind. Write IPv6 addresses with brackets, for example `"[::]"`.
      '';
    };

    port = mkOption {
      type = types.port;
      default = 8080;
      description = "TCP port to listen on.";
    };

    feedTTL = mkOption {
      type = types.str;
      default = "30s";
      example = "2m";
      description = "How long a magpie response stays fresh before it is fetched again (`FEED_TTL`).";
    };

    magpie = {
      url = mkOption {
        type = types.nullOr types.str;
        default = null;
        example = "http://magpie.example:8081";
        description = ''
          Base URL of the magpie instance. When null, the sheet renders without
          the Resources panel.
        '';
      };

      keyFile = mkOption {
        type = types.nullOr types.str;
        default = null;
        example = "/run/secrets/magpie-web-key";
        description = ''
          File holding `MAGPIE_WEB_KEY=...` for magpie's web API. systemd reads it
          before the service drops privileges, so it may be root-only. Required
          when {option}`services.nvirellia.magpie.url` is set.
        '';
      };
    };

    gatus = {
      url = mkOption {
        type = types.nullOr types.str;
        default = null;
        example = "http://127.0.0.1:3001";
        description = ''
          Base URL of the gatus instance whose uptime checks fill the Status
          tab. When null, the uptime half of the tab is omitted.
        '';
      };
    };

    prometheus = {
      url = mkOption {
        type = types.nullOr types.str;
        default = null;
        example = "http://127.0.0.1:9090";
        description = ''
          Base URL of the Prometheus HTTP API whose node probes fill the Status
          tab. When null, the probe half of the tab is omitted.
        '';
      };

      job = mkOption {
        type = types.str;
        default = "node";
        example = "node";
        description = ''
          Scrape job holding the node_exporter targets (`PROMETHEUS_JOB`). The
          targets should carry a `host` label with the display name.
        '';
      };
    };

    openFirewall = mkEnableOption "opening the configured port in the firewall";
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.magpie.url == null || cfg.magpie.keyFile != null;
        message = "services.nvirellia.magpie.keyFile is required when magpie.url is set.";
      }
      {
        assertion = cfg.magpie.keyFile == null || cfg.magpie.url != null;
        message = "services.nvirellia.magpie.keyFile has no effect without magpie.url.";
      }
      {
        assertion = cfg.prometheus.url == null || cfg.prometheus.job != "";
        message = "services.nvirellia.prometheus.job must not be empty.";
      }
    ];

    networking.firewall.allowedTCPPorts = optional cfg.openFirewall cfg.port;

    systemd.services.nvirellia = {
      description = "nvirellia.im profile sheet";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];

      environment = {
        LISTEN_ADDR = "${cfg.listenAddress}:${toString cfg.port}";
        FEED_TTL = cfg.feedTTL;
      }
      // optionalAttrs (cfg.magpie.url != null) {
        MAGPIE_URL = cfg.magpie.url;
      }
      // optionalAttrs (cfg.gatus.url != null) {
        GATUS_URL = cfg.gatus.url;
      }
      // optionalAttrs (cfg.prometheus.url != null) {
        PROMETHEUS_URL = cfg.prometheus.url;
        PROMETHEUS_JOB = cfg.prometheus.job;
      };

      serviceConfig = {
        ExecStart = "${cfg.package}/bin/nvirellia";
        EnvironmentFile = optional (cfg.magpie.keyFile != null) cfg.magpie.keyFile;

        DynamicUser = true;
        StateDirectory = "nvirellia";
        WorkingDirectory = "/var/lib/nvirellia";

        # Binding a privileged port is the only capability the app may need.
        AmbientCapabilities = optional (cfg.port < 1024) "CAP_NET_BIND_SERVICE";

        Restart = "on-failure";
        RestartSec = 5;

        NoNewPrivileges = true;
        PrivateDevices = true;
        PrivateTmp = true;
        ProtectHome = true;
        ProtectSystem = "strict";
        RestrictSUIDSGID = true;
      };
    };
  };
}

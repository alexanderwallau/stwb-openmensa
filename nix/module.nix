{ config, lib, pkgs, ... }:

let
  cfg = config.services.stwb-openmensa;
in
{
  options.services.stwb-openmensa = {
    enable = lib.mkEnableOption "OpenMensa-compatible parser server for Studierendenwerk Bonn";

    package = lib.mkOption {
      type = lib.types.package;
      description = "The stwb-openmensa package to use.";
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8080;
      description = "TCP port to listen on.";
    };

    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1";
      description = "Address to bind the HTTP server to.";
    };

    refreshTimes = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ "07:00" "11:00" "14:00" "17:00" ];
      example = [ "08:00" "12:00" "15:00" ];
      description = ''
        Times of day (HH:MM, local time) at which today's menu is re-fetched
        from studierendenwerk-bonn.de for every canteen.  The server caches
        responses for up to one year; past dates are never re-fetched.
      '';
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "stwb-openmensa";
      description = "Unix user to run the service as.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "stwb-openmensa";
      description = "Unix group to run the service as.";
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.${cfg.user} = lib.mkIf (cfg.user == "stwb-openmensa") {
      isSystemUser = true;
      group = cfg.group;
      description = "stwb-openmensa service user";
    };

    users.groups.${cfg.group} = lib.mkIf (cfg.group == "stwb-openmensa") { };

    systemd.services.stwb-openmensa = {
      description = "stwb-openmensa OpenMensa parser for Studierendenwerk Bonn";
      documentation = [ "https://github.com/alexanderwallau/stwb-openmensa" ];
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];

      serviceConfig = {
        ExecStart = lib.escapeShellArgs ([
          "${cfg.package}/bin/stwb-openmensa"
          "--port"    (toString cfg.port)
          "--listen"  cfg.listenAddress
          "--refresh" (lib.concatStringsSep "," cfg.refreshTimes)
        ]);

        User  = cfg.user;
        Group = cfg.group;

        Restart    = "on-failure";
        RestartSec = "10s";

        # Hardening
        NoNewPrivileges      = true;
        PrivateTmp           = true;
        ProtectSystem        = "strict";
        ProtectHome          = true;
        RestrictAddressFamilies = [ "AF_INET" "AF_INET6" ];
        RestrictNamespaces   = true;
        LockPersonality      = true;
        MemoryDenyWriteExecute = true;
        SystemCallFilter     = [ "@system-service" ];
      } // lib.optionalAttrs (cfg.port < 1024) {
        AmbientCapabilities = "CAP_NET_BIND_SERVICE";
      };
    };
  };
}

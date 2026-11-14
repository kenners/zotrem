self:
{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.zotrem;
in
{
  options.services.zotrem = {
    enable = lib.mkEnableOption "zotrem, two-way sync between Zotero and reMarkable";

    environment = lib.mkOption {
      type = lib.types.attrsOf lib.types.str;
      default = { };
      description = "Extra environment variables for the zotrem service.";
    };

    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1";
      example = "0.0.0.0";
      description = ''
        Address the web UI and API listen on. zotrem has no authentication, so anyone who can reach this
        address can control it and read the stored Zotero API key.
      '';
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Open the port on every interface. To expose zotrem on one interface only, leave this off and use
        networking.firewall.interfaces.<name>.allowedTCPPorts instead.
      '';
    };

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = lib.literalExpression "zotrem.packages.\${system}.default";
      description = "The zotrem package to run.";
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8000;
      description = "Port the web UI and API listen on.";
    };
  };

  config = lib.mkIf cfg.enable {
    networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall [ cfg.port ];

    systemd.services.zotrem = {
      after = [ "network-online.target" ];
      description = "zotrem Zotero and reMarkable sync";
      environment = {
        ZOTREM_DATA_DIR = "/var/lib/zotrem";
      }
      // cfg.environment;
      wantedBy = [ "multi-user.target" ];
      wants = [ "network-online.target" ];

      serviceConfig = {
        CapabilityBoundingSet = "";
        DynamicUser = true;
        ExecStart = "${lib.getExe cfg.package} serve --bind ${cfg.listenAddress} --port ${toString cfg.port}";
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        NoNewPrivileges = true;
        PrivateDevices = true;
        PrivateTmp = true;
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectHome = true;
        ProtectHostname = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        ProtectSystem = "strict";
        Restart = "on-failure";
        RestartSec = 5;
        RestrictAddressFamilies = [
          "AF_INET"
          "AF_INET6"
          "AF_UNIX"
        ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        StateDirectory = "zotrem";
        StateDirectoryMode = "0700";
        SystemCallArchitectures = "native";
        SystemCallFilter = [
          "@system-service"
          "~@privileged"
        ];
        UMask = "0077";
      };
    };
  };
}

{ module, pkgs }:

pkgs.testers.runNixOSTest {
  name = "zotrem";

  nodes.machine = {
    imports = [ module ];
    services.zotrem.enable = true;
  };

  testScript = ''
    machine.wait_for_unit("zotrem.service")
    machine.wait_for_open_port(8000)
    assert "zotrem" in machine.succeed("curl --fail -s http://127.0.0.1:8000/")
    assert "healthcheck_url" in machine.succeed("curl --fail -s http://127.0.0.1:8000/settings")
    machine.succeed("curl --fail -s http://127.0.0.1:8000/api/status")
    machine.succeed("test \"$(stat -c %a /var/lib/private/zotrem/zotrem.db)\" = 600")
    machine.succeed("systemctl restart zotrem.service")
    machine.wait_for_unit("zotrem.service")
    machine.wait_for_open_port(8000)
    machine.fail("journalctl -u zotrem.service | grep -Ei 'bad system call|SIGSYS|permission denied'")
  '';
}

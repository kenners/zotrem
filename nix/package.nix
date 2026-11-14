{
  buildGoModule,
  lib,
  rev ? "unknown",
  vendorHash,
  version,
}:

buildGoModule {
  pname = "zotrem";
  inherit vendorHash version;

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../cmd
      ../go.mod
      ../go.sum
      ../pkg
      ../testkit
      ../uikit
    ];
  };

  proxyVendor = true;
  subPackages = [ "cmd/zotrem" ];

  preBuild = ''
    go tool templ generate
    (cd pkg/db && go tool sqlc generate)
  '';

  ldflags = [
    "-s"
    "-w"
    "-X main.commit=${rev}"
    "-X main.version=${version}"
  ];

  checkPhase = ''
    runHook preCheck
    go test ./pkg/...
    runHook postCheck
  '';

  meta = {
    description = "Two-way sync between Zotero and reMarkable";
    homepage = "https://github.com/kenners/zotrem";
    license = lib.licenses.mit;
    mainProgram = "zotrem";
  };
}

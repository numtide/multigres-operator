{ pkgs }:
pkgs.mkShell {
  # Add build dependencies
  packages = with pkgs; [
    go
    nodejs
  ];

  # Add environment variables
  env = { };

  # Load custom bash code
  shellHook = ''

  '';
}

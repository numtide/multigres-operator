{ pkgs }:
pkgs.mkShell {
  # Add build dependencies
  packages = with pkgs; [ go ];

  # Add environment variables
  env = { };

  # Load custom bash code
  shellHook = ''

  '';
}

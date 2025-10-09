{ pkgs }:
pkgs.mkShell {
  # Add build dependencies
  packages = with pkgs; [
    go
    kubebuilder

    # For some script use cases
    nodejs
  ];

  # Add environment variables
  env = { };

  # Load custom bash code
  shellHook = ''

  '';
}

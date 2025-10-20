{ pkgs }:
pkgs.mkShell rec {
  # Add build dependencies
  packages = with pkgs; [
    go
    kubebuilder
    docker
    docker-buildx
    kubectl
    kind
    golangci-lint

    # For some script use cases
    nodejs
  ];

  # Add environment variables
  env = {
    "ENVTEST_K8S_VERSION"= "1.33";  # Default version for Nix users
  };

  # Load custom bash code
  shellHook = ''
    export KUBEBUILDER_ASSETS="$PWD/bin/k8s/${env.ENVTEST_K8S_VERSION}.0-${pkgs.go.GOOS}-${pkgs.go.GOARCH}"
  '';
}

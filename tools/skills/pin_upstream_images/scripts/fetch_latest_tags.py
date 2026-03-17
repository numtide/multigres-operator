#!/usr/bin/env python3
"""Fetch the latest SHA tag for each multigres GHCR image that shares the 'main' tag.

Uses the GHCR OCI Distribution API. No authentication needed for public packages.
Runs digest lookups in parallel for speed.

Output: KEY=VALUE pairs, one per line:
  MULTIGRES_TAG=sha-XXXXXXX
  PGCTLD_TAG=sha-XXXXXXX
  MULTIADMIN_WEB_TAG=sha-XXXXXXX
"""

import json
import sys
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

REGISTRY = "ghcr.io"

IMAGES = {
    "MULTIGRES": "multigres/multigres",
    "PGCTLD": "multigres/pgctld",
    "MULTIADMIN_WEB": "multigres/multiadmin-web",
}

ACCEPT = ",".join([
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
    "application/vnd.docker.distribution.manifest.v2+json",
])


def get_token(image: str) -> str:
    url = f"https://{REGISTRY}/token?scope=repository:{image}:pull"
    with urllib.request.urlopen(url) as resp:
        return json.loads(resp.read())["token"]


def get_digest(image: str, token: str, ref: str) -> str:
    url = f"https://{REGISTRY}/v2/{image}/manifests/{ref}"
    req = urllib.request.Request(url, method="HEAD", headers={
        "Authorization": f"Bearer {token}",
        "Accept": ACCEPT,
    })
    with urllib.request.urlopen(req) as resp:
        return resp.headers.get("Docker-Content-Digest", "")


def get_all_tags(image: str, token: str) -> list[str]:
    url = f"https://{REGISTRY}/v2/{image}/tags/list?n=10000"
    req = urllib.request.Request(url, headers={"Authorization": f"Bearer {token}"})
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read()).get("tags", [])


def find_sha_tag(image: str, token: str, target_digest: str, sha_tags: list[str]) -> str | None:
    """Find the sha-* tag matching target_digest using parallel lookups."""
    with ThreadPoolExecutor(max_workers=20) as pool:
        future_to_tag = {
            pool.submit(get_digest, image, token, tag): tag
            for tag in sha_tags
        }
        for future in as_completed(future_to_tag):
            tag = future_to_tag[future]
            if future.result() == target_digest:
                return tag
    return None


def main():
    results = {}
    errors = []

    for var_name, image in IMAGES.items():
        token = get_token(image)
        main_digest = get_digest(image, token, "main")
        if not main_digest:
            errors.append(f"Could not resolve digest for {image}:main")
            continue

        all_tags = get_all_tags(image, token)
        sha_tags = [t for t in all_tags if t.startswith("sha-")]

        found = find_sha_tag(image, token, main_digest, sha_tags)
        if not found:
            errors.append(
                f"No sha-* tag matching main for {image} "
                f"(digest={main_digest}, checked {len(sha_tags)} tags)"
            )
            continue

        results[var_name] = found

    # Print results in a stable order
    for var_name in IMAGES:
        if var_name in results:
            print(f"{var_name}_TAG={results[var_name]}")

    if errors:
        for e in errors:
            print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()

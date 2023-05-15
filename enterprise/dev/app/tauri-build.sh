#!/usr/bin/env bash
set -eu

cd "$(dirname "${BASH_SOURCE[0]}")"/../../.. || exit 1

download_artifacts() {
  mkdir -p .bin
  buildkite-agent artifact download ".bin/sourcegraph-backend-*" .bin/
}

set_version() {
  local version
  local tauri_conf
  local tmp
  version=$1
  tauri_conf="./src-tauri/tauri.conf.json"
  tmp=$(mktemp)
  echo "--- updating package version in '${tauri_conf}' to ${version}"
  jq --arg version "${version}" '.package.version = $version' "${tauri_conf}" > "${tmp}"
  mv "${tmp}" ./src-tauri/tauri.conf.json
}

github_release() {
  mkdir -p dist
  src=$(find ./src-tauri/target/release -type f \( -name "*.dmg" -o -name "*.deb" -o -name "*.AppImage" -o -name "*.tar.gz" \) -exec realpath {} \;)
  for f in ${src}; do
    mv -vf $f "./dist/${f}"
  done

  echo "--- Creating GitHub release for Sourcegraph App (${VERSION})"
  echo "Release will have to following assets:"
  ls -al ./dist
  gh release create -d -p ${VERSION} --notes "generated release from buildkite" "./dist/*"
}

if [[ ${CI:-""} == "true" ]]; then
  download_artifacts
fi

VERSION=$(./enterprise/dev/app/app_version.sh)
set_version ${VERSION}

echo "--- [Tauri] Building Application (${VERSION})"]
NODE_ENV=production pnpm run build-app-shell
pnpm tauri build

if [[ ${CI} == "true" ]];
  github_release
fi

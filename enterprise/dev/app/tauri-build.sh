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
  src=$(find ./src-tauri/target/release -type f \( -name "*.dmg" -o -name "*.deb" -o -name "*.AppImage" -o -name "*.tar.gz" \))
  for f in ${src}; do
    mv $f "dist/${f}"
  done

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

github_release

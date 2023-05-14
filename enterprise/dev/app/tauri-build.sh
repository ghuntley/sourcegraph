#!/usr/bin/env bash
set -eu

cd "$(dirname "${BASH_SOURCE[0]}")"/../../.. || exit 1

download_artifacts() {
  for artifact in "sourcegraph-backend-x86_64-unknown-linux-gnu"; do
    buildkite-agent artifact download .bin/${artifact} .bin/${artifact}
  done
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

if [[ ${CI:-""} == "true" ]]; then
  download_artifacts
fi

VERSION=$(./enterprise/dev/app/app_version.sh)
set_version ${VERSION}
echo "--- [Tauri] Building Application (${VERSION})"]
NODE_ENV=production pnpm run build-app-shell
pnpm tauri build

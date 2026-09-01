#!/bin/sh

set -eu

repository="Zhangs-11/agent-preview"
install_directory="${AGENT_PREVIEW_INSTALL_DIR:-${HOME}/.local/bin}"

case "$(uname -s)" in
  Darwin) operating_system="darwin" ;;
  Linux) operating_system="linux" ;;
  *) echo "agent-preview does not provide a binary for $(uname -s)." >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) architecture="arm64" ;;
  x86_64|amd64) architecture="amd64" ;;
  *) echo "agent-preview does not provide a binary for $(uname -m)." >&2; exit 1 ;;
esac

asset="agent-preview_${operating_system}_${architecture}.tar.gz"
release_base="https://github.com/${repository}/releases/latest/download"
temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/agent-preview-install.XXXXXX")"
archive="${temporary_directory}/${asset}"
checksums="${temporary_directory}/SHA256SUMS"

cleanup() {
  rm -rf "${temporary_directory}"
}
trap cleanup EXIT HUP INT TERM

curl -fsSL "${release_base}/${asset}" -o "${archive}"
curl -fsSL "${release_base}/SHA256SUMS" -o "${checksums}"

expected="$(awk -v name="${asset}" '$2 == name {print $1}' "${checksums}")"
if [ -z "${expected}" ]; then
  echo "No checksum was published for ${asset}." >&2
  exit 1
fi

if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "${archive}" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${archive}" | awk '{print $1}')"
else
  echo "Install shasum or sha256sum before installing agent-preview." >&2
  exit 1
fi

if [ "${actual}" != "${expected}" ]; then
  echo "SHA-256 verification failed for ${asset}." >&2
  exit 1
fi

mkdir -p "${install_directory}"
tar -xzf "${archive}" -C "${temporary_directory}"
install -m 0755 "${temporary_directory}/agent-preview" "${install_directory}/agent-preview"
"${install_directory}/agent-preview" version >/dev/null

echo "agent-preview was installed at ${install_directory}/agent-preview"
echo "Public previews can use localhost.run through the system SSH client."
echo "To prefer Cloudflare, review its terms and optionally run:"
echo "  agent-preview setup --accept-cloudflare-terms"

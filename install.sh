#!/bin/sh
# Installs the latest aesthetics release for macOS or Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/sadasant/aesthetics/main/install.sh | sh
#
# Set AESTHETICS_INSTALL_DIR to choose where the binary goes
# (default: /usr/local/bin if writable, otherwise ~/.local/bin).
set -eu

repo="sadasant/aesthetics"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "aesthetics: unsupported system $(uname -s); download a release from https://github.com/$repo/releases" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "aesthetics: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

dir="${AESTHETICS_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
fi

archive="aesthetics_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/latest/download"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $archive…"
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

expected="$(grep " $archive\$" "$tmp/checksums.txt" | cut -d ' ' -f 1)"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive" | cut -d ' ' -f 1)"
else
  actual="$(shasum -a 256 "$tmp/$archive" | cut -d ' ' -f 1)"
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "aesthetics: checksum mismatch for $archive" >&2
  exit 1
fi

tar -xzf "$tmp/$archive" -C "$tmp" aesthetics
mkdir -p "$dir"
mv "$tmp/aesthetics" "$dir/aesthetics"
chmod +x "$dir/aesthetics"
echo "Installed $("$dir/aesthetics" version) to $dir/aesthetics"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to your PATH to run aesthetics from anywhere." ;;
esac

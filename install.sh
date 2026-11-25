#!/bin/sh
# Installs the latest patrolbox release into /usr/local/bin (or $PATROLBOX_DIR).
set -eu

repo="ferhalemre/patrolbox"
dir="${PATROLBOX_DIR:-/usr/local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) echo "patrolbox: unsupported system: $os" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv7l|armv6l) arch=arm ;;
  *) echo "patrolbox: unsupported architecture: $arch" >&2; exit 1 ;;
esac

file="patrolbox-$os-$arch"
base="https://github.com/$repo/releases/latest/download"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $file ..."
curl -fsSL "$base/$file" -o "$tmp/$file"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

want=$(grep " $file\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$file" | cut -d' ' -f1)
else
  got=$(shasum -a 256 "$tmp/$file" | cut -d' ' -f1)
fi
if [ -z "$want" ] || [ "$want" != "$got" ]; then
  echo "patrolbox: checksum mismatch, not installing" >&2
  exit 1
fi

chmod +x "$tmp/$file"
if [ -w "$dir" ]; then
  mv "$tmp/$file" "$dir/patrolbox"
else
  sudo mv "$tmp/$file" "$dir/patrolbox"
fi

echo "Installed $("$dir/patrolbox" version) to $dir/patrolbox"
echo "Next: patrolbox discover"

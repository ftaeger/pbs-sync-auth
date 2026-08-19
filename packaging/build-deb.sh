#!/usr/bin/env bash
# Build the pbs-sync-auth-client Debian package from a pre-built client binary.
#
# Usage:
#   packaging/build-deb.sh <binary> <version> [arch] [outdir]
#
#   <binary>   path to the compiled linux client binary (matching <arch>)
#   <version>  Debian package version, e.g. 1.2.3 or 0.0.0~gitabc1234
#   [arch]     Debian architecture (default: amd64)
#   [outdir]   directory to write the .deb into (default: current directory)
#
# Requires dpkg-deb (present on Debian/Ubuntu; on macOS run inside a debian
# container, e.g. `docker run --rm -v "$PWD":/w -w /w debian:trixie ...`).
set -euo pipefail

bin="${1:?binary path required}"
version="${2:?version required}"
arch="${3:-amd64}"
outdir="${4:-.}"

here="$(cd "$(dirname "$0")" && pwd)"
src="$here/debian"
pkg="pbs-sync-auth-client"

[ -f "$bin" ] || { echo "binary not found: $bin" >&2; exit 1; }
command -v dpkg-deb >/dev/null || { echo "dpkg-deb not found (install dpkg or use a debian container)" >&2; exit 1; }

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
chmod 0755 "$stage"   # mktemp defaults to 0700; keep the package root at 0755

# --- installed filesystem layout ---
install -d -m 0755 "$stage/usr/bin"
install -m 0755 "$bin" "$stage/usr/bin/pbs-auth-client"

install -d -m 0755 "$stage/lib/systemd/system"
install -m 0644 "$src/pbs-sync-auth.service" "$stage/lib/systemd/system/pbs-sync-auth.service"

install -d -m 0755 "$stage/etc/pbs-sync-auth"
install -m 0644 "$src/client.conf" "$stage/etc/pbs-sync-auth/client.conf"

# --- control area ---
install -d -m 0755 "$stage/DEBIAN"
sed -e "s/@VERSION@/$version/g" -e "s/@ARCH@/$arch/g" \
  "$src/control" > "$stage/DEBIAN/control"
install -m 0644 "$src/conffiles" "$stage/DEBIAN/conffiles"
install -m 0755 "$src/postinst" "$stage/DEBIAN/postinst"
install -m 0755 "$src/prerm"    "$stage/DEBIAN/prerm"
install -m 0755 "$src/postrm"   "$stage/DEBIAN/postrm"

mkdir -p "$outdir"
deb="$outdir/${pkg}_${version}_${arch}.deb"
dpkg-deb --root-owner-group --build "$stage" "$deb"

echo "built: $deb"
dpkg-deb --info "$deb"
dpkg-deb --contents "$deb"

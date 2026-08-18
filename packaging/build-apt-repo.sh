#!/usr/bin/env bash
# Build a signed APT repository from a directory of .deb files.
#
# Usage:
#   packaging/build-apt-repo.sh <deb-dir> <out-dir> <signing-fingerprint>
#
#   <deb-dir>   directory containing the .deb files to include
#   <out-dir>   directory to write the published repo tree into (recreated)
#   <fpr>       GPG fingerprint of the signing key (must be in the keyring)
#
# Produces a clean tree (dists/ + pool/ + pubkey.asc + index.html) suitable for
# static hosting; the reprepro conf/ and db/ are kept out of it. Requires
# reprepro and a gpg keyring that holds the signing secret key.
set -euo pipefail

debdir="${1:?deb dir required}"
outdir="${2:?out dir required}"
fpr="${3:?signing key fingerprint required}"

here="$(cd "$(dirname "$0")" && pwd)"
src="$here/apt"

command -v reprepro >/dev/null || { echo "reprepro not found" >&2; exit 1; }

shopt -s nullglob
debs=("$debdir"/*.deb)
[ ${#debs[@]} -gt 0 ] || { echo "no .deb files in $debdir" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/conf"
sed "s/@FPR@/$fpr/g" "$src/distributions" > "$work/conf/distributions"

for d in "${debs[@]}"; do
  echo "including: $(basename "$d")"
  reprepro -b "$work" includedeb stable "$d"
done

# Assemble the clean publish tree (no conf/ or db/).
rm -rf "$outdir"
mkdir -p "$outdir"
cp -r "$work/dists" "$work/pool" "$outdir/"
cp "$src/pubkey.asc" "$outdir/pubkey.asc"
sed "s/@FPR@/$fpr/g" "$src/index.html" > "$outdir/index.html"
touch "$outdir/.nojekyll"   # serve the tree verbatim on GitHub Pages

echo "=== published files ==="
find "$outdir" -type f | sort
echo "=== dists/stable/Release ==="
cat "$outdir/dists/stable/Release"

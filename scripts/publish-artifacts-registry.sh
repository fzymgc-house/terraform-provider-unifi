#!/usr/bin/env bash
# Publish a signed GoReleaser build of this provider to the static OpenTofu provider registry at
# https://artifacts.fzymgc.house (design D10 of openspec/changes/exit-hcp-terraform in
# fzymgc-house/selfhosted-cluster).
#
# Usage: scripts/publish-artifacts-registry.sh DIST_DIR VERSION
#
# DIST_DIR holds terraform-provider-unifi_<VERSION>_SHA256SUMS, its .sig, and one zip per
# platform. The environment holds:
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY  the key artifacts-rw, from
#       secret/fzymgc-house/infrastructure/nas/rustfs-artifacts-rw
#   GPG_KEY_ID, GPG_PUBLIC_KEY  the key_id and public_key fields of
#       secret/fzymgc-house/infrastructure/terraform-registry/signing-key
#
# The objects live in the RustFS bucket artifacts and are written through s3.nas.fzymgc.house.
# The script writes in a fixed order, so the registry never names a version whose files are
# missing: the zip files, SHA256SUMS and its signature first, then the download file of each
# platform, and the versions file last. If versions already names VERSION, the script stops
# and changes nothing. A failed run can be run again, because it only writes the same files.
set -euo pipefail

dist="$1"
version="$2"
: "${GPG_KEY_ID:?}" "${GPG_PUBLIC_KEY:?}" "${AWS_ACCESS_KEY_ID:?}" "${AWS_SECRET_ACCESS_KEY:?}"

namespace="fzymgc-house"
name="unifi"
host="https://artifacts.fzymgc.house"
endpoint="${S3_ENDPOINT:-https://s3.nas.fzymgc.house}"
bucket="artifacts"
api="terraform/providers/v1/${namespace}/${name}"
files="terraform/files/${namespace}/${name}/${version}"
prefix="terraform-provider-${name}_${version}"
export AWS_REGION="${AWS_REGION:-us-east-1}"

s3() { aws --endpoint-url "$endpoint" s3 "$@"; }
put_json() { s3 cp --only-show-errors --content-type application/json - "s3://${bucket}/$1"; }

protocols="$(jq -c '.metadata.protocol_versions' "$(dirname "$0")/../terraform-registry-manifest.json")"

# Start from an empty list only when the versions file does not exist. Any other error stops the
# script, so a bad key never replaces the list of published versions.
if head="$(aws --endpoint-url "$endpoint" s3api head-object --bucket "$bucket" --key "${api}/versions" 2>&1)"; then
	versions="$(s3 cp --only-show-errors "s3://${bucket}/${api}/versions" -)"
elif [[ "$head" == *"(404)"* ]]; then
	versions='{"versions":[]}'
else
	echo "cannot read ${api}/versions: ${head}" >&2
	exit 1
fi
if jq -e --arg v "$version" '.versions[] | select(.version == $v)' <<<"$versions" >/dev/null; then
	echo "version ${version} is already published; nothing changed"
	exit 0
fi

(cd "$dist" && shasum -a 256 -c "${prefix}_SHA256SUMS" --ignore-missing)
gpg_dir="$(mktemp -d)"
trap 'rm -rf "$gpg_dir"' EXIT
GNUPGHOME="$gpg_dir" gpg --batch --quiet --import <<<"$GPG_PUBLIC_KEY"
GNUPGHOME="$gpg_dir" gpg --batch --quiet --verify "${dist}/${prefix}_SHA256SUMS.sig" "${dist}/${prefix}_SHA256SUMS"

for f in "${dist}/${prefix}"_*.zip "${dist}/${prefix}_SHA256SUMS" "${dist}/${prefix}_SHA256SUMS.sig"; do
	s3 cp --only-show-errors "$f" "s3://${bucket}/${files}/$(basename "$f")"
done
echo "files uploaded"

platforms="[]"
for zip in "${dist}/${prefix}"_*.zip; do
	file="$(basename "$zip")"
	platform="${file#"${prefix}_"}"
	platform="${platform%.zip}"
	os="${platform%%_*}"
	arch="${platform#*_}"
	shasum="$(awk -v f="$file" '$2 == f {print $1}' "${dist}/${prefix}_SHA256SUMS")"
	jq -n --argjson p "$protocols" --arg os "$os" --arg arch "$arch" --arg f "$file" --arg s "$shasum" \
		--arg base "${host}/${files}" --arg sums "${prefix}_SHA256SUMS" \
		--arg kid "$GPG_KEY_ID" --arg key "$GPG_PUBLIC_KEY" \
		'{protocols: $p, os: $os, arch: $arch, filename: $f, shasum: $s,
		  download_url: "\($base)/\($f)",
		  shasums_url: "\($base)/\($sums)",
		  shasums_signature_url: "\($base)/\($sums).sig",
		  signing_keys: {gpg_public_keys: [{key_id: $kid, ascii_armor: $key}]}}' |
		put_json "${api}/${version}/download/${os}/${arch}"
	platforms="$(jq -c --arg os "$os" --arg arch "$arch" '. + [{os: $os, arch: $arch}]' <<<"$platforms")"
	echo "platform ${os}/${arch} written"
done

jq -n '{"providers.v1": "/terraform/providers/v1/"}' | put_json ".well-known/terraform.json"
jq -c --arg v "$version" --argjson p "$protocols" --argjson pl "$platforms" \
	'.versions += [{version: $v, protocols: $p, platforms: $pl}]' <<<"$versions" |
	put_json "${api}/versions"
echo "version ${version} published"

#!/usr/bin/env bash
# Upload a signed GoReleaser build of this provider to the HCP Terraform private registry.
#
# Usage: TFE_TOKEN=... scripts/publish-hcp-registry.sh DIST_DIR VERSION KEY_ID
#
# DIST_DIR holds terraform-provider-unifi_<VERSION>_SHA256SUMS, its .sig, and one zip per
# platform. KEY_ID is the registry key-id of the signing key (output provider_signing_key_id
# of tf/hcp-terraform in fzymgc-house/selfhosted-cluster). The provider entry and the key must
# already exist in the registry. A version or platform that already exists is left alone, so a
# failed upload can be re-run.
set -euo pipefail

dist="$1"
version="$2"
key_id="$3"
org="${HCP_ORG:-fzymgc-house}"
name="unifi"
prefix="terraform-provider-${name}_${version}"
api="https://app.terraform.io/api/v2/organizations/${org}/registry-providers/private/${org}/${name}"

call() {
	local method="$1" url="$2" body="${3:-}"
	curl --silent --show-error --fail-with-body \
		--header "Authorization: Bearer ${TFE_TOKEN}" \
		--header "Content-Type: application/vnd.api+json" \
		--request "$method" ${body:+--data "$body"} "$url"
}

upload() {
	curl --silent --show-error --fail-with-body --upload-file "$2" "$1" >/dev/null
}

if version_json="$(call GET "${api}/versions/${version}" 2>/dev/null)"; then
	echo "version ${version} exists"
else
	version_json="$(call POST "${api}/versions" "$(jq -n --arg v "$version" --arg k "$key_id" \
		'{data: {type: "registry-provider-versions", attributes: {version: $v, "key-id": $k, protocols: ["6.0"]}}}')")"
	echo "version ${version} created"
fi

if [ "$(jq -r '.data.attributes["shasums-uploaded"]' <<<"$version_json")" != "true" ]; then
	upload "$(jq -r '.data.links["shasums-upload"]' <<<"$version_json")" "${dist}/${prefix}_SHA256SUMS"
	echo "SHA256SUMS uploaded"
fi
if [ "$(jq -r '.data.attributes["shasums-sig-uploaded"]' <<<"$version_json")" != "true" ]; then
	upload "$(jq -r '.data.links["shasums-sig-upload"]' <<<"$version_json")" "${dist}/${prefix}_SHA256SUMS.sig"
	echo "SHA256SUMS.sig uploaded"
fi

for zip in "${dist}/${prefix}"_*.zip; do
	file="$(basename "$zip")"
	platform="${file#"${prefix}_"}"
	platform="${platform%.zip}"
	os="${platform%%_*}"
	arch="${platform#*_}"
	shasum="$(awk -v f="$file" '$2 == f {print $1}' "${dist}/${prefix}_SHA256SUMS")"

	if platform_json="$(call GET "${api}/versions/${version}/platforms/${os}/${arch}" 2>/dev/null)"; then
		echo "platform ${os}/${arch} exists"
	else
		platform_json="$(call POST "${api}/versions/${version}/platforms" "$(jq -n --arg os "$os" --arg arch "$arch" \
			--arg s "$shasum" --arg f "$file" \
			'{data: {type: "registry-provider-version-platforms", attributes: {os: $os, arch: $arch, shasum: $s, filename: $f}}}')")"
		echo "platform ${os}/${arch} created"
	fi
	if [ "$(jq -r '.data.attributes["provider-binary-uploaded"]' <<<"$platform_json")" != "true" ]; then
		upload "$(jq -r '.data.links["provider-binary-upload"]' <<<"$platform_json")" "$zip"
		echo "platform ${os}/${arch} binary uploaded"
	fi
done

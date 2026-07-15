#!/bin/sh
set -eu

period=${FLOWLENS_GEOIP_PERIOD:-$(date -u +%Y-%m)}
destination=${FLOWLENS_GEOIP_DIR:-/var/lib/flowlens/geoip}
reload_url=${FLOWLENS_GEOIP_RELOAD_URL:-http://127.0.0.1:8088/api/v1/admin/geoip/reload}
base_url=https://download.db-ip.com/free
country_archive="dbip-country-lite-${period}.mmdb.gz"
asn_archive="dbip-asn-lite-${period}.mmdb.gz"

mkdir -p "$destination"
temporary=$(mktemp -d "$destination/.update.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

curl -fsS --retry 3 -o "$temporary/$country_archive" "$base_url/$country_archive"
curl -fsS --retry 3 -o "$temporary/$asn_archive" "$base_url/$asn_archive"
gzip -t "$temporary/$country_archive"
gzip -t "$temporary/$asn_archive"
gzip -dc "$temporary/$country_archive" > "$temporary/country.mmdb"
gzip -dc "$temporary/$asn_archive" > "$temporary/asn.mmdb"
chmod 0644 "$temporary/country.mmdb" "$temporary/asn.mmdb"

had_country=false
had_asn=false
if [ -f "$destination/country.mmdb" ]; then
  cp -p "$destination/country.mmdb" "$temporary/country.backup"
  had_country=true
fi
if [ -f "$destination/asn.mmdb" ]; then
  cp -p "$destination/asn.mmdb" "$temporary/asn.backup"
  had_asn=true
fi

rollback() {
  if [ "$had_country" = true ]; then
    mv -f "$temporary/country.backup" "$destination/country.mmdb"
  else
    rm -f "$destination/country.mmdb"
  fi
  if [ "$had_asn" = true ]; then
    mv -f "$temporary/asn.backup" "$destination/asn.mmdb"
  else
    rm -f "$destination/asn.mmdb"
  fi
}

mv -f "$temporary/country.mmdb" "$destination/country.mmdb"
mv -f "$temporary/asn.mmdb" "$destination/asn.mmdb"

token=${FLOWLENS_AGENT_TOKEN:-}
if [ -z "$token" ] && [ -r /etc/flowlens/agent.env ]; then
  token=$(awk -F= '$1 == "FLOWLENS_AGENT_TOKEN" { print substr($0, index($0, "=") + 1); exit }' /etc/flowlens/agent.env)
fi
if [ -z "$token" ]; then
  rollback
  echo "FLOWLENS_AGENT_TOKEN is required to reload GeoIP databases" >&2
  exit 1
fi
if ! curl -fsS -X POST -H "Authorization: Bearer $token" "$reload_url" >/dev/null; then
  rollback
  curl -fsS -X POST -H "Authorization: Bearer $token" "$reload_url" >/dev/null 2>&1 || true
  echo "GeoIP reload failed; previous databases restored" >&2
  exit 1
fi

printf '%s\n' 'IP Geolocation by DB-IP: https://db-ip.com' 'DB-IP Lite license: https://creativecommons.org/licenses/by/4.0/' > "$destination/LICENSE.txt"

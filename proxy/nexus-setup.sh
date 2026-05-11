#!/usr/bin/env bash
# nexus-setup.sh — one-shot Nexus configuration after first boot.
#
# Usage:
#   ./nexus-setup.sh [NEW_ADMIN_PASSWORD]
#
# NEW_ADMIN_PASSWORD defaults to "admin123" if not provided.
# Run from the proxy/ directory (or anywhere — it only needs curl + docker).
#
# What it does:
#   1. Reads the initial random password from the Nexus container
#   2. Waits until Nexus REST API is ready
#   3. Changes the admin password to NEW_ADMIN_PASSWORD
#   4. Enables anonymous read access
#   5. Creates proxy repos: go, pypi, npm, maven-central, gradle-plugins
#
# Idempotent: repo creation returns 400 if already exists — that is treated
# as success so the script is safe to re-run.

set -euo pipefail

NEXUS_URL="${NEXUS_URL:-http://localhost:8081}"
NEXUS_CONTAINER="${NEXUS_CONTAINER:-proxy-nexus-1}"
NEW_PASSWORD="${NEXUS_ADMIN_PASSWORD:-${1:-admin123}}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[nexus-setup]${NC} $*"; }
warn() { echo -e "${YELLOW}[nexus-setup]${NC} $*"; }
err()  { echo -e "${RED}[nexus-setup]${NC} $*" >&2; }

# ---------------------------------------------------------------------------
# 1. Read initial admin password
# ---------------------------------------------------------------------------
INIT_PASSWORD_FILE="/nexus-data/admin.password"

log "Reading initial admin password from container '${NEXUS_CONTAINER}'..."
INIT_PASSWORD=$(docker exec "${NEXUS_CONTAINER}" cat "${INIT_PASSWORD_FILE}" 2>/dev/null || true)

if [[ -z "${INIT_PASSWORD}" ]]; then
  # File is removed by Nexus after first-time setup completes — nothing to do.
  log "No initial password file found — Nexus already configured. Exiting."
  exit 0
fi

log "Initial password retrieved."

# ---------------------------------------------------------------------------
# 2. Wait for Nexus to be ready
# ---------------------------------------------------------------------------
log "Waiting for Nexus at ${NEXUS_URL} ..."
MAX_WAIT=180
ELAPSED=0
until curl -sf "${NEXUS_URL}/service/rest/v1/status" -o /dev/null; do
  if (( ELAPSED >= MAX_WAIT )); then
    err "Nexus did not become ready within ${MAX_WAIT}s."
    exit 1
  fi
  sleep 5
  ELAPSED=$(( ELAPSED + 5 ))
  echo -n "."
done
echo ""
log "Nexus is ready."

CURL_AUTH=(-u "admin:${INIT_PASSWORD}")

# ---------------------------------------------------------------------------
# 3. Change admin password
# ---------------------------------------------------------------------------
log "Setting admin password..."
HTTP=$(curl -sf -o /dev/null -w "%{http_code}" \
  -X PUT "${NEXUS_URL}/service/rest/v1/security/users/admin/change-password" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: text/plain" \
  --data-raw "${NEW_PASSWORD}" || true)

if [[ "${HTTP}" == "204" ]]; then
  log "Password changed successfully."
  CURL_AUTH=(-u "admin:${NEW_PASSWORD}")
else
  err "Unexpected HTTP ${HTTP} when changing password."
  exit 1
fi

# ---------------------------------------------------------------------------
# 4. Enable anonymous read access
# ---------------------------------------------------------------------------
log "Enabling anonymous read access..."
HTTP=$(curl -sf -o /dev/null -w "%{http_code}" \
  -X PUT "${NEXUS_URL}/service/rest/v1/security/anonymous" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{"enabled": true, "userId": "anonymous", "realmName": "NexusAuthorizingRealm"}' || true)

if [[ "${HTTP}" == "200" ]]; then
  log "Anonymous access enabled."
else
  err "Failed to enable anonymous access. HTTP: ${HTTP}"
  exit 1
fi

# ---------------------------------------------------------------------------
# 5. Create proxy repositories
# ---------------------------------------------------------------------------
create_repo() {
  local name="$1"
  local body="$2"

  HTTP=$(curl -sf -o /dev/null -w "%{http_code}" \
    -X POST "${NEXUS_URL}/service/rest/v1/repositories/${3}/proxy" \
    "${CURL_AUTH[@]}" \
    -H "Content-Type: application/json" \
    -d "${body}" || true)

  if [[ "${HTTP}" == "201" ]]; then
    log "Created repo: ${name}"
  elif [[ "${HTTP}" == "400" ]]; then
    warn "Repo already exists (skipping): ${name}"
  else
    err "Failed to create repo '${name}'. HTTP: ${HTTP}"
    exit 1
  fi
}

# -- Go modules (raw proxy) --------------------------------------------------
create_repo "go-proxy" '{
  "name": "go-proxy",
  "online": true,
  "storage": {"blobStoreName": "default", "strictContentTypeValidation": false},
  "proxy": {
    "remoteUrl": "https://proxy.golang.org",
    "contentMaxAge": 1440,
    "metadataMaxAge": 1440
  },
  "negativeCache": {"enabled": true, "timeToLive": 1440},
  "httpClient": {"blocked": false, "autoBlock": false}
}' "raw"

# -- PyPI --------------------------------------------------------------------
create_repo "pypi-proxy" '{
  "name": "pypi-proxy",
  "online": true,
  "storage": {"blobStoreName": "default", "strictContentTypeValidation": false},
  "proxy": {
    "remoteUrl": "https://pypi.org",
    "contentMaxAge": 1440,
    "metadataMaxAge": 1440
  },
  "negativeCache": {"enabled": true, "timeToLive": 1440},
  "httpClient": {"blocked": false, "autoBlock": false}
}' "pypi"

# -- npm (also used by Bun) --------------------------------------------------
create_repo "npm-proxy" '{
  "name": "npm-proxy",
  "online": true,
  "storage": {"blobStoreName": "default", "strictContentTypeValidation": false},
  "proxy": {
    "remoteUrl": "https://registry.npmjs.org",
    "contentMaxAge": 1440,
    "metadataMaxAge": 1440
  },
  "negativeCache": {"enabled": true, "timeToLive": 1440},
  "httpClient": {"blocked": false, "autoBlock": false}
}' "npm"

# -- Maven central -----------------------------------------------------------
create_repo "maven-proxy" '{
  "name": "maven-proxy",
  "online": true,
  "storage": {"blobStoreName": "default", "strictContentTypeValidation": false, "writePolicy": "allow"},
  "proxy": {
    "remoteUrl": "https://repo1.maven.org/maven2",
    "contentMaxAge": 1440,
    "metadataMaxAge": 1440
  },
  "negativeCache": {"enabled": true, "timeToLive": 1440},
  "httpClient": {"blocked": false, "autoBlock": false},
  "maven": {"versionPolicy": "RELEASE", "layoutPolicy": "STRICT", "contentDisposition": "ATTACHMENT"}
}' "maven"

# -- Gradle plugins ----------------------------------------------------------
create_repo "gradle-plugins-proxy" '{
  "name": "gradle-plugins-proxy",
  "online": true,
  "storage": {"blobStoreName": "default", "strictContentTypeValidation": false, "writePolicy": "allow"},
  "proxy": {
    "remoteUrl": "https://plugins.gradle.org/m2",
    "contentMaxAge": 1440,
    "metadataMaxAge": 1440
  },
  "negativeCache": {"enabled": true, "timeToLive": 1440},
  "httpClient": {"blocked": false, "autoBlock": false},
  "maven": {"versionPolicy": "RELEASE", "layoutPolicy": "STRICT", "contentDisposition": "ATTACHMENT"}
}' "maven"

# ---------------------------------------------------------------------------
log "Done. Nexus is configured and ready."
log "  URL:  ${NEXUS_URL}"
log "  User: admin"
log ""
log "Repo paths:"
log "  Go:              ${NEXUS_URL}/repository/go-proxy/"
log "  PyPI:            ${NEXUS_URL}/repository/pypi-proxy/simple/"
log "  npm:             ${NEXUS_URL}/repository/npm-proxy/"
log "  Maven:           ${NEXUS_URL}/repository/maven-proxy/"
log "  Gradle plugins:  ${NEXUS_URL}/repository/gradle-plugins-proxy/"

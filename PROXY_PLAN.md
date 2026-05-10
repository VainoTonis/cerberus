# Proxy Plan

## Goal

Agent containers have no direct internet access. All outbound traffic is
forced through one of two local services:

- Nexus — caching proxy for package managers (Go, pip, npm, Maven/Gradle)
- Squid — forward proxy for everything else (API calls, e.g. AWS Bedrock)

First package download fetches from upstream and caches in Nexus. Every
subsequent hit is local. API traffic goes through Squid which enforces an
allowlist — anything not explicitly permitted is blocked.

## Docker networking

Two networks:

    sandbox-internal  — internal: true, no internet routing
    nexus-external    — default bridge, has internet access

Agent containers attach to sandbox-internal only. With internal: true Docker
enforces no external routing at the kernel level — it is not just convention.

Nexus and Squid attach to both networks: reachable by agents on
sandbox-internal, can reach the internet on nexus-external.

    networks:
      sandbox-internal:
        internal: true
      nexus-external:
        driver: bridge

    services:
      nexus:
        networks:
          - sandbox-internal
          - nexus-external

      squid:
        networks:
          - sandbox-internal
          - nexus-external

Agent containers are created by the sandbox manager and attached to
sandbox-internal only — never to nexus-external.

## Traffic flow

    agent -> Nexus:8081          package downloads (Go, pip, npm, Maven)
    agent -> Squid:3128          all other outbound (API calls, etc.)
    Nexus -> internet            upstream package registries (cache misses)
    Squid -> internet            allowlisted domains only
    agent -> internet            blocked (no route)

## Nexus (package caching)

Sonatype Nexus Repository Community Edition (free). Single node, local
filesystem blob storage. No paid features required for this use case.

### Repositories to create

All served from port 8081 under different paths.

| Ecosystem        | Nexus repo type  | Upstream URL                       |
|------------------|------------------|------------------------------------|
| Go modules       | Raw (HTTP proxy) | https://proxy.golang.org           |
| Python (pip)     | PyPI proxy       | https://pypi.org                   |
| Node.js / Bun    | npm proxy        | https://registry.npmjs.org         |
| Gradle (Maven)   | Maven proxy      | https://repo1.maven.org/maven2     |
| Gradle (plugins) | Maven proxy      | https://plugins.gradle.org/m2      |

Bun uses the npm registry format — no extra config needed beyond the npm
proxy repo.

Go has no native Nexus format. A raw proxy repo caches the HTTP responses
from proxy.golang.org. Sufficient for local caching.

### Docker Compose entry

    nexus:
      image: sonatype/nexus3
      environment:
        - INSTALL4J_ADD_VM_PARAMS=-Xms512m -Xmx1g -XX:MaxDirectMemorySize=512m
      volumes:
        - nexus-data:/nexus-data
      ports:
        - "8081:8081"
      networks:
        - sandbox-internal
        - nexus-external

Pin JVM heap explicitly or Nexus will claim as much RAM as it can find.
512m-1g is sufficient for local caching.

### First boot gotcha

Nexus writes a random admin password on first start to:

    /nexus-data/admin.password

Retrieve it, log in at http://localhost:8081, complete the setup wizard,
then enable anonymous read access so agent containers need no credentials.

### One-time setup steps

1. Start Nexus via Docker Compose (creates networks automatically)
2. Wait ~2 minutes for JVM init
3. docker exec <nexus> cat /nexus-data/admin.password
4. Log in at http://localhost:8081, complete wizard
5. Settings > Security > Anonymous — enable anonymous read
6. Settings > Repositories > Create for each repo in the table above

## Squid (general outbound proxy)

Squid uses CONNECT tunneling for HTTPS — it sees the destination hostname
but not the content. No SSL bumping, no local CA cert required. Sufficient
to enforce an allowlist by domain.

### squid.conf (allowlist approach)

Everything not explicitly allowed is denied:

    acl allowed_domains dstdomain .amazonaws.com
    acl allowed_domains dstdomain .openai.com
    # add more as needed

    http_access allow allowed_domains
    http_access deny all

    http_port 3128

Edit squid.conf on the host and restart the Squid container to update the
allowlist. No agent changes needed.

### Docker Compose entry

    squid:
      image: ubuntu/squid
      volumes:
        - ./squid.conf:/etc/squid/squid.conf:ro
      networks:
        - sandbox-internal
        - nexus-external

## Agent container config (env vars injected at container creation)

    GOPROXY=http://nexus:8081/repository/go-proxy/,off
    PIP_INDEX_URL=http://nexus:8081/repository/pypi-proxy/simple/
    PIP_TRUSTED_HOST=nexus
    NPM_CONFIG_REGISTRY=http://nexus:8081/repository/npm-proxy/
    GRADLE_USER_HOME=/root/.gradle
    HTTP_PROXY=http://squid:3128
    HTTPS_PROXY=http://squid:3128
    NO_PROXY=nexus

GOPROXY ends with ,off (not ,direct) — if Nexus doesn't have the module Go
returns an error instead of attempting a direct internet fetch.

NO_PROXY=nexus ensures package manager traffic goes directly to Nexus on
the internal network without being routed through Squid.

## Gradle/Maven: init script

Env vars alone are not enough for Gradle — it reads repository URLs from
build files which may declare their own upstreams. An init script overrides
all repository declarations globally.

Mount read-only into every agent container at:

    /root/.gradle/init.d/nexus.gradle

Contents:

    allprojects {
        buildscript {
            repositories {
                maven { url "http://nexus:8081/repository/maven-proxy/" }
                maven { url "http://nexus:8081/repository/gradle-plugins-proxy/" }
            }
        }
        repositories {
            maven { url "http://nexus:8081/repository/maven-proxy/" }
            maven { url "http://nexus:8081/repository/gradle-plugins-proxy/" }
        }
    }

Agents hit Nexus regardless of what upstream URLs their build.gradle declares.

## Verifying isolation

    # Should fail (no direct route, internal: true network)
    docker exec <agent> curl --noproxy '*' -m 5 https://google.com

    # Should fail (google.com not in Squid allowlist)
    docker exec <agent> curl -m 5 https://google.com

    # Should succeed (allowlisted domain, routed via Squid)
    docker exec <agent> curl -m 5 https://bedrock.us-east-1.amazonaws.com

    # Should succeed (cached via Nexus)
    docker exec <agent> go get golang.org/x/sync

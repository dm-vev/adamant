#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
JAVA_HOME=${JAVA_HOME:-$HOME/.cache/adamant-vanilla/jdk-25.0.4.1}
MAPPED_JAR=${MAPPED_JAR:-$HOME/.cache/adamant-vanilla/MCP-Reborn-26.2/projects/mcp/build/mcp/merge/output.jar}
VERSION_MANIFEST=${VERSION_MANIFEST:-$HOME/.cache/adamant-vanilla/MCP-Reborn-26.2/projects/mcp/build/mcp/downloadJson/version.json}
LIBRARY_CACHE=${LIBRARY_CACHE:-$HOME/.gradle/caches/forge_gradle/maven_downloader}
CLIENT_JAR=${CLIENT_JAR:-$HOME/.cache/adamant-vanilla/MCP-Reborn-26.2/projects/mcp/build/mcp/downloadClient/client.jar}

for tool in "$JAVA_HOME/bin/java" "$JAVA_HOME/bin/javac" jq; do
    if [[ "$tool" == */* ]]; then
        [[ -x "$tool" ]] || { echo "missing executable: $tool" >&2; exit 1; }
    else
        command -v "$tool" >/dev/null || { echo "missing executable: $tool" >&2; exit 1; }
    fi
done
[[ -f "$MAPPED_JAR" ]] || { echo "missing mapped Java jar: $MAPPED_JAR" >&2; exit 1; }
[[ -f "$VERSION_MANIFEST" ]] || { echo "missing Java version manifest: $VERSION_MANIFEST" >&2; exit 1; }
[[ -f "$CLIENT_JAR" ]] || { echo "missing vanilla resource jar: $CLIENT_JAR" >&2; exit 1; }
manifest_version=$(jq -er '.id | select(type == "string")' "$VERSION_MANIFEST")
[[ "$manifest_version" == "26.3" ]] || { echo "expected Java 26.3 manifest, got $manifest_version" >&2; exit 1; }

mapfile -t libraries < <(jq -r '.libraries[] | select(.downloads.artifact.path != null) | .downloads.artifact.path' "$VERSION_MANIFEST")
(( ${#libraries[@]} > 0 )) || { echo "version manifest contains no library artifacts" >&2; exit 1; }
classpath="$MAPPED_JAR:$CLIENT_JAR"
for path in "${libraries[@]}"; do
    jar="$LIBRARY_CACHE/$path"
    [[ -f "$jar" ]] || { echo "missing manifest library: $jar" >&2; exit 1; }
    classpath+=":$jar"
done

build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
"$JAVA_HOME/bin/javac" --release 25 -cp "$classpath" -d "$build" "$ROOT/cmd/vanilla-enchantment-export/JavaEnchantmentExporter.java"
"$JAVA_HOME/bin/java" -Djava.awt.headless=true -Dadamant.enchantments.expectedVersion="$manifest_version" -cp "$build:$classpath" tools.vanillaenchantment.JavaEnchantmentExporter

#!/usr/bin/env bash
# Выпуск новой версии Binder.
#   ./scripts/release.sh 1.0.1 "Что изменилось"
# Поднимает версию в main.go и wails.json, собирает exe, коммитит, ставит тег,
# пушит и создаёт релиз на GitHub с exe — по нему обновятся все биндеры.
set -euo pipefail
cd "$(dirname "$0")/.."

VER="${1:?укажите версию, например 1.0.1}"
NOTES="${2:-Версия $VER}"

if ! [[ "$VER" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "версия должна быть вида 1.2.3" >&2; exit 1
fi
if git rev-parse -q --verify "refs/tags/v$VER" >/dev/null; then
  echo "тег v$VER уже есть" >&2; exit 1
fi

sed -i "s/appVersion = \"[^\"]*\"/appVersion = \"$VER\"/" main.go
sed -i "s/\"productVersion\": \"[^\"]*\"/\"productVersion\": \"$VER\"/" wails.json

wails build -platform windows/amd64 -ldflags "-s -w"
mkdir -p build/release
cp "build/bin/T31K Binder.exe" build/release/Binder.exe

git add -A
git commit -m "Версия $VER" -m "$NOTES"
git tag "v$VER"
git push origin main "v$VER"
gh release create "v$VER" build/release/Binder.exe \
  --repo tthreeonek/binder --title "Binder $VER" --notes "$NOTES"

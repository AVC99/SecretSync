#!/bin/sh
# Instala secretsync desde GitHub Releases (macOS y Linux).
#
#   curl -fsSL https://raw.githubusercontent.com/AVC99/secretsync/main/install.sh | sh
#
# Variables opcionales:
#   VERSION=v0.1.0      versión concreta (por defecto, la última)
#   INSTALL_DIR=/ruta   dónde dejar el binario (por defecto /usr/local/bin si
#                       se puede escribir, si no ~/.local/bin)
set -eu

REPO="AVC99/secretsync"
BINARY="secretsync"

err() {
	echo "error: $*" >&2
	exit 1
}

case "$(uname -s)" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) err "sistema no soportado: $(uname -s). Descarga el binario de https://github.com/$REPO/releases" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) err "arquitectura no soportada: $(uname -m)" ;;
esac

if [ -n "${VERSION:-}" ]; then
	base="https://github.com/$REPO/releases/download/$VERSION"
else
	base="https://github.com/$REPO/releases/latest/download"
fi

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
else
	err "hace falta curl o wget"
fi

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	err "hace falta sha256sum o shasum"
fi

if [ -z "${INSTALL_DIR:-}" ]; then
	if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		INSTALL_DIR=/usr/local/bin
	else
		INSTALL_DIR="$HOME/.local/bin"
	fi
fi

asset="${BINARY}_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Descargando ${asset}…"
fetch "$base/$asset" "$tmp/$asset" || err "no se pudo descargar $base/$asset"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || err "no se pudo descargar checksums.txt"

want="$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || err "$asset no aparece en checksums.txt"
[ "$(sha256 "$tmp/$asset")" = "$want" ] || err "el checksum de $asset no coincide"

tar -xzf "$tmp/$asset" -C "$tmp" "$BINARY"
mkdir -p "$INSTALL_DIR"
mv "$tmp/$BINARY" "$INSTALL_DIR/$BINARY"
chmod 755 "$INSTALL_DIR/$BINARY"

echo "✔ $("$INSTALL_DIR/$BINARY" --version) instalado en $INSTALL_DIR/$BINARY"
case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	echo "  $INSTALL_DIR no está en tu PATH. Añádelo con:"
	case "${SHELL:-}" in
	*/zsh) rc="~/.zshrc" ;;
	*/bash) rc="~/.bashrc" ;;
	*) rc="tu fichero de arranque del shell" ;;
	esac
	echo "    echo 'export PATH=\"\$PATH:$INSTALL_DIR\"' >> $rc"
	;;
esac

#!/usr/bin/env bash
# mktestvol.sh — Create or restore a lightweight test volume from a Blu-ray disc.
#
# Copies PLAYLIST and CLIPINF metadata verbatim (small files), then creates
# sparse stub files for STREAM/*.m2ts that match the originals in size but
# contain no video data. This is enough for spindrift to run correctly since
# it only reads file sizes (for duration estimation), never stream content.
#
# Two files per volume are committed to git:
#   metadata.tar  — BDMV metadata (playlists, clip info, bdmv files, disc title XML)
#   sizes.txt     — stream filename + size pairs, stored inside the tar
#
# The BDMV/ directory itself is gitignored; use --restore to recreate it.
#
# Usage:
#   ./mktestvol.sh [/Volumes/DiscName ...]   # rip from disc(s)
#   ./mktestvol.sh --restore [name ...]      # recreate BDMV from metadata.tar + stubs
#
# With no arguments, auto-detects all mounted Blu-ray discs under /Volumes.

set -euo pipefail

DEST_ROOT="$(dirname "$0")/test_volumes"

# ── Restore mode ─────────────────────────────────────────────────────────────
if [ "${1:-}" = "--restore" ]; then
    shift
    targets=()
    if [ $# -eq 0 ]; then
        for d in "$DEST_ROOT"/*/; do
            [ -f "$d/metadata.tar" ] && targets+=("$d")
        done
        if [ ${#targets[@]} -eq 0 ]; then
            echo "No test volumes with metadata.tar found in $DEST_ROOT" >&2
            exit 1
        fi
    else
        for name in "$@"; do
            targets+=("$DEST_ROOT/$name")
        done
    fi

    for vol in "${targets[@]}"; do
        name="$(basename "$vol")"
        tar="$vol/metadata.tar"
        if [ ! -f "$tar" ]; then
            echo "==> $name: no metadata.tar, skipping" >&2
            continue
        fi
        echo "==> $name (restoring)"
        [ -d "$vol/BDMV" ] && chmod -R u+w "$vol/BDMV"
        rm -rf "$vol/BDMV"
        (cd "$vol" && tar xf metadata.tar)

        count=0
        while IFS=' ' read -r fname size; do
            truncate -s "$size" "$vol/BDMV/STREAM/$fname"
            count=$((count + 1))
        done < "$vol/BDMV/STREAM/sizes.txt"
        echo "    $count stream stubs restored"
    done
    exit 0
fi

# ── Rip mode ──────────────────────────────────────────────────────────────────

# Auto-detect if no arguments given
if [ $# -eq 0 ]; then
    discs=()
    for d in /Volumes/*/BDMV; do
        [ -d "$d" ] && discs+=("$(dirname "$d")")
    done
    if [ ${#discs[@]} -eq 0 ]; then
        echo "No Blu-ray discs found under /Volumes" >&2
        exit 1
    fi
    set -- "${discs[@]}"
fi

for DISC in "$@"; do
    NAME="$(basename "$DISC")"
    DEST="$DEST_ROOT/$NAME"

    echo "==> $NAME"
    [ -d "$DEST" ] && chmod -R u+w "$DEST"
    rm -rf "$DEST"
    mkdir -p "$DEST/BDMV/PLAYLIST" "$DEST/BDMV/CLIPINF" "$DEST/BDMV/STREAM"

    # BDMV root metadata
    cp "$DISC/BDMV/"*.bdmv "$DEST/BDMV/" 2>/dev/null || true
    chmod u+w "$DEST/BDMV/"*.bdmv 2>/dev/null || true

    # Disc title XML (used for TMDB lookup)
    if [ -d "$DISC/BDMV/META" ]; then
        cp -r "$DISC/BDMV/META" "$DEST/BDMV/"
        chmod -R u+w "$DEST/BDMV/META"
    fi

    # Playlist and clip info: copy verbatim
    cp "$DISC/BDMV/PLAYLIST/"*.mpls "$DEST/BDMV/PLAYLIST/"
    chmod u+w "$DEST/BDMV/PLAYLIST/"*.mpls
    echo "    PLAYLIST: $(ls "$DEST/BDMV/PLAYLIST/"*.mpls | wc -l | tr -d ' ') files"

    cp "$DISC/BDMV/CLIPINF/"*.clpi "$DEST/BDMV/CLIPINF/"
    chmod u+w "$DEST/BDMV/CLIPINF/"*.clpi
    echo "    CLIPINF:  $(ls "$DEST/BDMV/CLIPINF/"*.clpi | wc -l | tr -d ' ') files"

    # Streams: sparse stubs + sizes.txt manifest
    count=0
    total=0
    : > "$DEST/BDMV/STREAM/sizes.txt"
    for f in "$DISC/BDMV/STREAM/"*.m2ts; do
        fname="$(basename "$f")"
        size="$(stat -f%z "$f")"
        truncate -s "$size" "$DEST/BDMV/STREAM/$fname"
        echo "$fname $size" >> "$DEST/BDMV/STREAM/sizes.txt"
        count=$((count + 1))
        total=$((total + size))
    done
    echo "    STREAM:   $count stubs ($(python3 -c "print(f'{$total/1024**3:.1f}') ") GiB represented)"

    # Bundle all metadata (no .m2ts stubs) into a single tar for git
    (cd "$DEST" && tar cf metadata.tar --exclude='*.m2ts' --exclude='*.jpg' --exclude='*.jpeg' --exclude='*.png' --exclude='*.bmp' --exclude='*.gif' BDMV)
    echo "    metadata.tar: $(du -sh "$DEST/metadata.tar" | cut -f1)"
    echo "    -> $DEST"
    echo "    Ejecting $NAME..."
    diskutil eject "$DISC" || true
done

// Package sourcemark stores a source node's custom feed mark — a service logo
// or similar — as a normalized square PNG under the app data dir. It is the
// webhook connector's image counterpart to the curated glyph set in
// internal/app/icons: a webhook node may show an uploaded image instead of a
// Lucide glyph.
//
// Unlike profileimg, which keys one avatar per flow id, marks are
// content-addressed: Set returns the stored bytes' hash and writes <hash>.png,
// and a node's config records that hash. The hash is written at upload time —
// before the graph save that records it — so keying the file by content rather
// than by node id lets the upload endpoint stay a pure bytes -> hash function
// with no node identity to thread, and identical images share one file.
//
// A missing file is not an error: a hash referenced with no file on disk (a
// flow synced to a machine without its data dir) reads as no mark, and the feed
// falls back to the node's glyph — the same tolerance profileimg's letter-chip
// fallback relies on. Orphaned blobs (a mark replaced or its node deleted) are
// left in place; they are small and harmless, never a reason to fail an edit.
//
// Normalization — contain-fit onto a transparent square, canonical PNG
// re-encode — lives here rather than in a caller so every setter produces the
// same stored shape. It fits rather than center-crops (profileimg's choice) so
// a wide logo is letterboxed intact instead of having its edges cut off.
package sourcemark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"

	_ "image/gif"  // register GIF decoder for image.Decode
	_ "image/jpeg" // register JPEG decoder for image.Decode

	_ "golang.org/x/image/webp" // register WebP decoder for image.Decode
)

const (
	// MaxInputBytes caps a raw upload before decoding, so a hostile or huge
	// file is rejected cheaply rather than decoded into memory.
	MaxInputBytes = 8 << 20

	// maxInputSide rejects an image whose decoded dimensions are implausibly
	// large — a decode-bomb guard well above any real logo source.
	maxInputSide = 12000

	// markSide is the square edge of the stored PNG. The feed renders it at
	// ~16px; 128 stays crisp on a HiDPI display while keeping the file tiny.
	markSide = 128

	// HashLen is the length of a mark reference: hex of the first 16 bytes of
	// the stored PNG's SHA-256. It is exported so a connector config can
	// validate a recorded reference's shape without decoding the store.
	HashLen = 32
)

var (
	// ErrEmpty is returned when raw carries no bytes.
	ErrEmpty = errors.New("sourcemark: image is empty")
	// ErrTooLarge is returned when raw exceeds MaxInputBytes or decodes to
	// implausible dimensions.
	ErrTooLarge = errors.New("sourcemark: image is too large")
	// ErrUnsupported is returned when raw cannot be decoded as a known image
	// format (PNG, JPEG, GIF, WebP).
	ErrUnsupported = errors.New("sourcemark: unsupported image format")
)

// Store keeps normalized mark PNGs under dir, one per content hash.
type Store struct {
	dir string
}

// NewStore returns a store rooted at dir (typically
// <StateDir>/assets/webhookmarks). The directory is created on first write.
func NewStore(dir string) *Store { return &Store{dir: dir} }

// Set normalizes raw into a square PNG, writes it as <hash>.png, and returns
// the stored bytes' content hash — the value a node's config records. Storing
// the same image again is a harmless overwrite with the same hash.
func (s *Store) Set(raw []byte) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", ErrEmpty
	}
	if len(raw) > MaxInputBytes {
		return "", ErrTooLarge
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", ErrUnsupported
	}
	b := src.Bounds()
	if b.Dx() > maxInputSide || b.Dy() > maxInputSide {
		return "", ErrTooLarge
	}

	out, err := encodeSquarePNG(src)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(out)
	hash := hex.EncodeToString(sum[:16])
	if err := writeFileAtomic(s.path(hash), out); err != nil {
		return "", err
	}
	return hash, nil
}

// Get returns the stored PNG for hash, or ok=false when none is stored. A
// malformed hash reads as absent rather than an error, so a hand-edited or
// stale reference falls back to the glyph instead of failing the read.
func (s *Store) Get(hash string) (data []byte, ok bool, err error) {
	if !ValidHash(hash) {
		return nil, false, nil
	}
	data, err = os.ReadFile(s.path(hash))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("sourcemark: read %s: %w", hash, err)
	}
	return data, true, nil
}

func (s *Store) path(hash string) string {
	return filepath.Join(s.dir, hash+".png")
}

// ValidHash reports whether s has the shape Set produces: lowercase hex of
// length HashLen. It also guards Get against path traversal, since a mark
// reference is user-supplied config.
func ValidHash(s string) bool {
	if len(s) != HashLen {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// encodeSquarePNG scales src to fit within a markSide square, centers it on a
// transparent canvas of that size, and returns canonical PNG bytes. Fitting
// (not cropping) keeps a non-square logo intact; the transparent margin lets it
// sit in the feed's mark slot like a glyph.
func encodeSquarePNG(src image.Image) ([]byte, error) {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	longest := max(sw, sh)
	dw := max(sw*markSide/longest, 1)
	dh := max(sh*markSide/longest, 1)
	offX := (markSide - dw) / 2
	offY := (markSide - dh) / 2

	dst := image.NewRGBA(image.Rect(0, 0, markSide, markSide))
	draw.CatmullRom.Scale(dst, image.Rect(offX, offY, offX+dw, offY+dh), src, b, draw.Src, nil)

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, dst); err != nil {
		return nil, fmt.Errorf("sourcemark: encode png: %w", err)
	}
	return buf.Bytes(), nil
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("sourcemark: create dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("sourcemark: write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("sourcemark: replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package images

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/pdftext"
)

// Kind is what sort of attachment a file is.
type Kind string

// The kinds of attachment.
const (
	KindImage    Kind = "image"
	KindDocument Kind = "document" // a PDF
	KindText     Kind = "text"
	KindAudio    Kind = "audio"
	KindVideo    Kind = "video"
)

// MediaType is a type of file Blitz attaches: its media type, kind and
// the extensions it goes by (the first is its usual one).
type MediaType struct {
	MIME string
	Kind Kind
	Exts []string
}

// Media are the attachable types (spec_images_011): what the providers
// take as input, Gemini the most of them. Which a model takes is the
// engine's to say (runtime.AcceptedMedia).
var Media = []MediaType{
	{"image/png", KindImage, []string{".png"}},
	{"image/jpeg", KindImage, []string{".jpg", ".jpeg"}},
	{"image/gif", KindImage, []string{".gif"}},
	{"image/webp", KindImage, []string{".webp"}},
	{"image/bmp", KindImage, []string{".bmp"}},
	{"image/heic", KindImage, []string{".heic"}},
	{"image/heif", KindImage, []string{".heif"}},
	{pdftext.MIME, KindDocument, []string{".pdf"}},
	{"text/plain", KindText, textExts},
	{"audio/mpeg", KindAudio, []string{".mp3"}},
	{"audio/wav", KindAudio, []string{".wav"}},
	{"audio/aac", KindAudio, []string{".aac"}},
	{"audio/flac", KindAudio, []string{".flac"}},
	{"audio/ogg", KindAudio, []string{".ogg", ".oga"}},
	{"audio/opus", KindAudio, []string{".opus"}},
	{"audio/mp4", KindAudio, []string{".m4a"}},
	{"audio/webm", KindAudio, []string{".weba"}},
	{"video/mp4", KindVideo, []string{".mp4", ".m4v"}},
	{"video/quicktime", KindVideo, []string{".mov"}},
	{"video/mpeg", KindVideo, []string{".mpeg", ".mpg"}},
	{"video/webm", KindVideo, []string{".webm"}},
	{"video/x-msvideo", KindVideo, []string{".avi"}},
	{"video/x-ms-wmv", KindVideo, []string{".wmv"}},
	{"video/x-flv", KindVideo, []string{".flv"}},
	{"video/3gpp", KindVideo, []string{".3gp"}},
}

// textExts are the file names that attach as text: notes, data and code.
var textExts = []string{
	".txt", ".md", ".markdown", ".csv", ".tsv", ".json", ".jsonl", ".xml", ".yaml", ".yml", ".toml", ".log", ".html", ".htm",
	".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".kt", ".c", ".h", ".cc", ".cpp", ".hpp", ".rs", ".rb", ".php",
	".swift", ".cs", ".scala", ".r", ".sql", ".sh", ".css", ".ipynb", ".tex", ".bib",
}

// MaxTextBytes caps a text attachment; MaxMediaBytes an audio or video one
// (the Gemini Files API's limit).
const (
	MaxTextBytes  = 1 << 20
	MaxMediaBytes = 2 << 30
)

// MediaOf is the attachable type with this media type (ok false for
// none).
func MediaOf(mime string) (MediaType, bool) {
	for _, m := range Media {
		if m.MIME == mime {
			return m, true
		}
	}
	return MediaType{}, false
}

// KindOf is the kind of an attachable media type ("" for none).
func KindOf(mime string) Kind {
	m, _ := MediaOf(mime)
	return m.Kind
}

// TypeForName is the attachable type a file name says ("" for none).
func TypeForName(name string) string {
	ext := strings.ToLower(path.Ext(strings.ReplaceAll(name, `\`, "/")))
	for _, m := range Media {
		if slices.Contains(m.Exts, ext) {
			return m.MIME
		}
	}
	return ""
}

// Detect is the attachable type of a file, from its first bytes (at least
// 512 where it has them), its name breaking ties: WebM is audio by its
// extension, else video; a file is text only by its name, and only when
// it's UTF-8 without NULs. "" when it's none of them.
func Detect(name string, head []byte) string {
	byName := TypeForName(name)
	switch {
	case pdftext.IsPDF(head):
		return pdftext.MIME
	case hasBrand(head):
		return isoType(head, byName)
	case bytes.HasPrefix(head, []byte("fLaC")):
		return "audio/flac"
	case bytes.HasPrefix(head, []byte("FLV")):
		return "video/x-flv"
	case bytes.HasPrefix(head, asfGUID):
		return "video/x-ms-wmv"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WAVE":
		return "audio/wav"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "AVI ":
		return "video/x-msvideo"
	case bytes.HasPrefix(head, []byte("OggS")):
		if bytes.Contains(head[:min(len(head), 64)], []byte("OpusHead")) {
			return "audio/opus"
		}
		return "audio/ogg"
	case bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}): // EBML: WebM, Matroska
		if KindOf(byName) == KindAudio {
			return "audio/webm"
		}
		return "video/webm"
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xF6 == 0xF0: // an ADTS AAC frame
		return "audio/aac"
	case bytes.HasPrefix(head, []byte("ID3")) || len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		return "audio/mpeg"
	case bytes.HasPrefix(head, []byte{0, 0, 1, 0xBA}) || bytes.HasPrefix(head, []byte{0, 0, 1, 0xB3}):
		return "video/mpeg"
	case bytes.HasPrefix(head, []byte("BM")) && byName == "image/bmp":
		return "image/bmp"
	}
	switch sniffed := http.DetectContentType(head); sniffed {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
		return sniffed
	}
	if KindOf(byName) == KindText && looksLikeText(head) {
		return "text/plain"
	}
	return ""
}

// asfGUID starts an ASF file (WMV, WMA).
var asfGUID = []byte{0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11}

// hasBrand reports an ISO base media file: a box size, then "ftyp".
func hasBrand(head []byte) bool {
	return len(head) >= 12 && string(head[4:8]) == "ftyp" && binary.BigEndian.Uint32(head[:4]) >= 8
}

// isoType tells ISO base media files apart by their major brand: HEIC and
// HEIF pictures, QuickTime, 3GPP, M4A audio, and MP4 for the rest (or M4A
// when the name says so).
func isoType(head []byte, byName string) string {
	switch brand := string(head[8:12]); {
	case brand == "heic" || brand == "heix" || brand == "hevc" || brand == "heim":
		return "image/heic"
	case brand == "mif1" || brand == "msf1":
		return "image/heif"
	case brand == "qt  ":
		return "video/quicktime"
	case strings.HasPrefix(brand, "3g"):
		return "video/3gpp"
	case brand == "M4A " || brand == "M4B ":
		return "audio/mp4"
	}
	if byName == "audio/mp4" {
		return byName
	}
	return "video/mp4"
}

// looksLikeText reports UTF-8 without NULs (a cut-off last character
// allowed: head may end mid-character).
func looksLikeText(head []byte) bool {
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	for i := 0; i < 3 && len(head) > 0 && !utf8.Valid(head); i++ {
		head = head[:len(head)-1]
	}
	return utf8.Valid(head)
}

// IsAttachablePath reports whether a file name is one an @mention
// attaches rather than inlines: a picture, a PDF, audio or video. (Text
// files are inlined by the mention itself.)
func IsAttachablePath(p string) bool {
	switch KindOf(TypeForName(p)) {
	case KindImage, KindDocument, KindAudio, KindVideo:
		return true
	}
	return false
}

// Duration is the length in seconds of WAV audio, or of an MP4,
// QuickTime, M4A or 3GPP file whose movie header (mvhd) is in data; 0
// when data doesn't give it.
func Duration(mime string, data []byte) float64 {
	switch mime {
	case "audio/wav":
		return wavSeconds(data)
	case "video/mp4", "video/quicktime", "audio/mp4", "video/3gpp":
		return mvhdSeconds(data)
	}
	return 0
}

// wavSeconds reads a RIFF WAVE file's fmt chunk (bytes a second) and its
// data chunk's size.
func wavSeconds(b []byte) float64 {
	var rate, size uint32
	for i := 12; i+8 <= len(b); {
		id, n := string(b[i:i+4]), binary.LittleEndian.Uint32(b[i+4:i+8])
		switch {
		case id == "fmt " && i+16 <= len(b):
			rate = binary.LittleEndian.Uint32(b[i+16 : i+20])
		case id == "data":
			size = n
		}
		if id == "data" || n > uint32(len(b)) {
			break
		}
		i += 8 + int(n) + int(n&1)
	}
	if rate == 0 || size == 0 {
		return 0
	}
	return float64(size) / float64(rate)
}

// mvhdSeconds finds a movie header box and reads its duration over its
// time scale (version 0: 32-bit fields; version 1: 64-bit).
func mvhdSeconds(b []byte) float64 {
	i := bytes.Index(b, []byte("mvhd"))
	if i < 4 || i+4+20 > len(b) { // a version 0 header's fields
		return 0
	}
	box := b[i+4:]
	var scale, dur uint64
	if box[0] == 1 {
		if len(box) < 32 {
			return 0
		}
		scale, dur = uint64(binary.BigEndian.Uint32(box[20:24])), binary.BigEndian.Uint64(box[24:32])
	} else {
		scale, dur = uint64(binary.BigEndian.Uint32(box[12:16])), uint64(binary.BigEndian.Uint32(box[16:20]))
	}
	if scale == 0 {
		return 0
	}
	return float64(dur) / float64(scale)
}

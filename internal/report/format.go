package report

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/klauspost/compress/zstd"
)

// ---------------------------------------------------------------------------
// On-disk chunk format
// ---------------------------------------------------------------------------
//
// A "segment" file (the active requests.zrc, or a rotated requests.zrc.N) is
// a flat, append-only sequence of self-contained "chunks":
//
//	Magic(4B) | FirstTS(8B f64) | LastTS(8B f64) | RecordCount(4B u32) |
//	Checkpoint(1B bool) | PayloadLen(4B u32) | zstd payload (PayloadLen bytes)
//
// The zstd payload holds the raw, concatenated JSONL bytes (each record
// terminated by '\n') for RecordCount records — exactly what used to be
// written directly to the old plain-text log file.
//
// Dictionary chaining
// --------------------
// Chunks are batched (see logger.go) so that most chunks hold more than one
// record: coding-agent traffic sends the full, ever-growing conversation
// history on every request, so consecutive records overlap almost entirely.
// Compressing each chunk independently wastes that overlap; instead, chunk N
// (N>1) is compressed using the *raw, decompressed* bytes of chunk N-1 as a
// zstd "raw content" dictionary (zstd.WithEncoderDictRaw). Measured on real
// ccrouter verbose logs this took the compressed size from ~48MB down to
// ~0.7MB for the same data (see format_test.go for the reference numbers).
//
// Checkpoint chunks
// -----------------
// If every chunk in a segment chained off the previous one, reading the most
// recent record would require decoding the *entire* segment from its first
// chunk onward — decode cost would grow without bound as the segment grows,
// which defeats the purpose of a fast "show me the last N requests" query.
//
// To bound this, every checkpointInterval-th chunk is a "checkpoint": it is
// compressed with NO dictionary (Checkpoint=true in the header), i.e. it can
// be decoded completely standalone. Reading any chunk therefore only ever
// requires decoding from the nearest preceding checkpoint forward — at most
// checkpointInterval chunks — regardless of how large the segment has grown.
//
// The very first chunk written to a fresh segment is always a checkpoint,
// which is also what makes segments independent of each other: deleting the
// oldest segment file (log rotation) never breaks any chunk in a newer one.
const (
	magicChunk    = uint32(0x5A524331)    // "ZRC1"
	chunkHeaderSz = 4 + 8 + 8 + 4 + 1 + 4 // magic+firstTS+lastTS+recordCount+checkpoint+payloadLen

	// checkpointInterval bounds how many chunks must be decoded to read an
	// arbitrary chunk: at most checkpointInterval-1 chained chunks after the
	// nearest checkpoint.
	checkpointInterval = 20
)

// dictID is the fixed zstd raw-dictionary slot used for chained chunks. Every
// chained chunk rebinds this same ID to a different byte slice (the previous
// chunk's raw content), so a constant ID is fine — it never needs to be
// globally unique, only consistent between the encode and matching decode
// call for that one chunk.
const dictID = uint32(1)

// encoderLevel is the compression level used for every chunk's zstd frame.
//
// SpeedBestCompression is the highest tier klauspost/compress/zstd exposes
// (a pure-Go implementation; roughly equivalent to the reference C encoder's
// level ~11). Benchmarked against real ccrouter verbose logs (coding-agent
// traffic, chained dictionary, 10 records/chunk):
//
//	SpeedFastest            ~18ms/chunk   5.4 MB total compressed
//	SpeedDefault             ~30ms/chunk   2.2 MB
//	SpeedBetterCompression   ~33ms/chunk   1.6 MB
//	SpeedBestCompression     ~95ms/chunk   0.7 MB   <- chosen
//
// The reference C zstd encoder (not available here; verified separately via
// the `zstd` CLI/python bindings on the same data) showed that going beyond
// this point (levels 13+) costs 40-50x more CPU for only ~15% extra
// compression — a bad trade for a background log writer that shares CPU with
// request handling. This is intentionally NOT exposed as a user-tunable
// setting: the level only affects the CPU cost of writing NEW chunks, never
// whether old data can be read back, so it is safe to change later purely as
// an internal constant without any migration concerns.
const encoderLevel = zstd.SpeedBestCompression

func newChunkEncoder(dict []byte) (*zstd.Encoder, error) {
	opts := []zstd.EOption{
		zstd.WithEncoderLevel(encoderLevel),
		// Single-shot EncodeAll on chunk-sized input (a handful of JSON
		// records): the async multi-goroutine pipeline isn't worth its
		// overhead here, so pin concurrency to 1.
		zstd.WithEncoderConcurrency(1),
		// A content checksum lets the decoder detect the case where a chunk
		// is decoded with the wrong dictionary: zstd dictionaries work by
		// letting sequences reference bytes "before" the frame by offset, so
		// decoding with mismatched dictionary content can otherwise succeed
		// structurally while silently producing garbage bytes instead of an
		// error. The checksum turns that into a hard decode failure.
		zstd.WithEncoderCRC(true),
	}
	if len(dict) > 0 {
		opts = append(opts, zstd.WithEncoderDictRaw(dictID, dict))
	}
	return zstd.NewWriter(nil, opts...)
}

func newChunkDecoder(dict []byte) (*zstd.Decoder, error) {
	opts := []zstd.DOption{zstd.WithDecoderConcurrency(1)}
	if len(dict) > 0 {
		opts = append(opts, zstd.WithDecoderDictRaw(dictID, dict))
	}
	return zstd.NewReader(nil, opts...)
}

// encodeChunk compresses raw (concatenated JSONL bytes for recordCount
// records) into a self-contained on-disk chunk. dict is the previous chunk's
// raw bytes (nil/empty for a checkpoint chunk).
func encodeChunk(raw []byte, recordCount int, firstTS, lastTS float64, dict []byte, checkpoint bool) ([]byte, error) {
	if checkpoint {
		dict = nil
	}
	enc, err := newChunkEncoder(dict)
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	payload := enc.EncodeAll(raw, nil)

	buf := make([]byte, chunkHeaderSz+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], magicChunk)
	binary.LittleEndian.PutUint64(buf[4:12], math.Float64bits(firstTS))
	binary.LittleEndian.PutUint64(buf[12:20], math.Float64bits(lastTS))
	binary.LittleEndian.PutUint32(buf[20:24], uint32(recordCount))
	if checkpoint {
		buf[24] = 1
	}
	binary.LittleEndian.PutUint32(buf[25:29], uint32(len(payload)))
	copy(buf[chunkHeaderSz:], payload)
	return buf, nil
}

// chunkMeta describes one chunk's header fields plus its on-disk location,
// as produced by scanning a segment file's headers (without decompressing
// payloads).
type chunkMeta struct {
	firstTS     float64
	lastTS      float64
	recordCount int
	checkpoint  bool
	payloadOff  int64 // absolute byte offset of the zstd payload in the file
	payloadLen  int64
}

func (m chunkMeta) totalSize() int64 { return int64(chunkHeaderSz) + m.payloadLen }

var errBadChunk = errors.New("report: corrupt chunk header")

// parseChunkHeader reads a chunk header from buf (must be at least
// chunkHeaderSz bytes, starting at the chunk's first byte).
func parseChunkHeader(buf []byte) (meta chunkMeta, err error) {
	if len(buf) < chunkHeaderSz {
		return chunkMeta{}, errBadChunk
	}
	if binary.LittleEndian.Uint32(buf[0:4]) != magicChunk {
		return chunkMeta{}, fmt.Errorf("%w: bad magic", errBadChunk)
	}
	meta.firstTS = math.Float64frombits(binary.LittleEndian.Uint64(buf[4:12]))
	meta.lastTS = math.Float64frombits(binary.LittleEndian.Uint64(buf[12:20]))
	meta.recordCount = int(binary.LittleEndian.Uint32(buf[20:24]))
	meta.checkpoint = buf[24] != 0
	meta.payloadLen = int64(binary.LittleEndian.Uint32(buf[25:29]))
	return meta, nil
}

// decodeChunkPayload decompresses one chunk's zstd payload given the raw
// content of the previous chunk as dict (nil if this chunk is a checkpoint).
func decodeChunkPayload(payload []byte, dict []byte) ([]byte, error) {
	dec, err := newChunkDecoder(dict)
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return dec.DecodeAll(payload, nil)
}

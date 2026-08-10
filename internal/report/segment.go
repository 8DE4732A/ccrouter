package report

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"
)

// scanChunkIndex reads all chunk headers in a segment file (without touching
// any payload bytes) and returns them in on-disk order. This is a cheap O(number
// of chunks) operation — it's what lets Read/ReadOne locate the right chunk(s)
// without decompressing the whole file.
func scanChunkIndex(path string) ([]chunkMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var metas []chunkMeta
	var offset int64
	hdr := make([]byte, chunkHeaderSz)
	for {
		if _, err := io.ReadFull(f, hdr); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		meta, err := parseChunkHeader(hdr)
		if err != nil {
			// Stop at the first corrupt/incomplete chunk (e.g. a partially
			// written chunk left behind by a crash mid-write) rather than
			// failing the whole segment — everything before it is still valid.
			break
		}
		meta.payloadOff = offset + chunkHeaderSz
		metas = append(metas, meta)
		offset += meta.totalSize()
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return metas, nil
}

// decodeCheckpointGroup decodes every chunk from the nearest checkpoint at or
// before idx, up through idx itself, in one forward pass, returning each
// chunk's decoded raw bytes in order (group[0] is the checkpoint chunk's
// content, group[len(group)-1] is idx's content). Callers that need only one
// chunk still benefit from this: it never decodes more than
// checkpointInterval chunks regardless of how large the segment is.
func decodeCheckpointGroup(f *os.File, metas []chunkMeta, idx int) ([][]byte, error) {
	start := idx
	for start > 0 && !metas[start].checkpoint {
		start--
	}

	group := make([][]byte, 0, idx-start+1)
	var dict []byte
	for i := start; i <= idx; i++ {
		payload := make([]byte, metas[i].payloadLen)
		if _, err := f.ReadAt(payload, metas[i].payloadOff); err != nil {
			return nil, err
		}
		raw, err := decodeChunkPayload(payload, dict)
		if err != nil {
			return nil, err
		}
		group = append(group, raw)
		dict = raw
	}
	return group, nil
}

// emitLines invokes fn for each non-blank line in raw.
func emitLines(raw []byte, fn func(line string)) {
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fn(line)
	}
}

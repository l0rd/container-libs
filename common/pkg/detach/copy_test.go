package detach

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	smallBytes = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	bigBytes   = []byte(strings.Repeat("0F", 32*1024+30))
)

func newCustomReader(buf *bytes.Buffer, readsize uint) *customReader {
	return &customReader{
		inner:    buf,
		readsize: readsize,
	}
}

type customReader struct {
	inner    *bytes.Buffer
	readsize uint
}

func (c *customReader) Read(p []byte) (n int, err error) {
	return c.inner.Read(p[:min(int(c.readsize), len(p))])
}

func FuzzCopy(f *testing.F) {
	for _, i := range []uint{1, 2, 3, 5, 10, 100, 200, 1000, 1024, 32 * 1024} {
		f.Add(i)
	}

	f.Fuzz(func(t *testing.T, readSize uint) {
		// 0 is not a valid read size
		if readSize == 0 {
			t.Skip()
		}

		tests := []struct {
			name         string
			from         []byte
			expected     []byte
			expectDetach bool
			keys         []byte
		}{
			{
				name:     "small copy",
				from:     smallBytes,
				expected: smallBytes,
				keys:     nil,
			},
			{
				name:     "small copy with detach keys",
				from:     smallBytes,
				expected: smallBytes,
				keys:     []byte{'A', 'B'},
			},
			{
				name:     "big copy",
				from:     bigBytes,
				expected: bigBytes,
				keys:     nil,
			},
			{
				name:     "big copy with detach keys",
				from:     bigBytes,
				expected: bigBytes,
				keys:     []byte{'A', 'B'},
			},
			{
				name:         "simple detach 1 key",
				from:         append(smallBytes, 'A'),
				expected:     smallBytes,
				expectDetach: true,
				keys:         []byte{'A'},
			},
			{
				name:         "simple detach 2 keys",
				from:         append(smallBytes, 'A', 'B'),
				expected:     smallBytes,
				expectDetach: true,
				keys:         []byte{'A', 'B'},
			},
			{
				name:         "simple detach 3 keys",
				from:         append(smallBytes, 'A', 'B', 'C'),
				expected:     smallBytes,
				expectDetach: true,
				keys:         []byte{'A', 'B', 'C'},
			},
			{
				name:         "detach early",
				from:         append(smallBytes, 'A', 'B', 'B', 'A'),
				expected:     smallBytes,
				expectDetach: true,
				keys:         []byte{'A', 'B'},
			},
			{
				name:         "detach with partial match",
				from:         append(smallBytes, 'A', 'A', 'A', 'B'),
				expected:     append(smallBytes, 'A', 'A'),
				expectDetach: true,
				keys:         []byte{'A', 'B'},
			},
			{
				name:         "big buffer detach with partial match",
				from:         append(bigBytes, 'A', 'A', 'A', 'B'),
				expected:     append(bigBytes, 'A', 'A'),
				expectDetach: true,
				keys:         []byte{'A', 'B'},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				dst := &bytes.Buffer{}
				src := newCustomReader(bytes.NewBuffer(tt.from), readSize)
				written, err := Copy(dst, src, tt.keys)
				if tt.expectDetach {
					assert.ErrorIs(t, err, ErrDetach)
				} else {
					assert.NoError(t, err)
				}
				assert.Equal(t, dst.Len(), int(written), "bytes written matches buffer")
				assert.Equal(t, tt.expected, dst.Bytes(), "buffer matches")
			})
		}
	})
}

var errUnexpectedRead = errors.New("unexpected read")

// chunkReader returns one chunk per Read, then fails. On a terminal a read
// past the input blocks until the user types again.
type chunkReader struct {
	chunks [][]byte
	reads  int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	c.reads++
	if len(c.chunks) == 0 {
		return 0, errUnexpectedRead
	}
	n := copy(p, c.chunks[0])
	c.chunks = c.chunks[1:]
	return n, nil
}

func TestCopyDetachWithoutFurtherInput(t *testing.T) {
	keys := []byte{'A', 'B'}
	tests := []struct {
		name     string
		chunks   [][]byte
		expected []byte
	}{
		{
			name:   "sequence alone in one read",
			chunks: [][]byte{{'A', 'B'}},
		},
		{
			name:     "sequence at the end of a read",
			chunks:   [][]byte{{'x', 'y', 'A', 'B'}},
			expected: []byte{'x', 'y'},
		},
		{
			name:     "sequence split across reads",
			chunks:   [][]byte{{'x', 'A'}, {'B'}},
			expected: []byte{'x'},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := &bytes.Buffer{}
			src := &chunkReader{chunks: tt.chunks}
			written, err := Copy(dst, src, keys)
			assert.ErrorIs(t, err, ErrDetach)
			assert.Equal(t, len(tt.chunks), src.reads, "Copy must detach without reading past the sequence")
			assert.Equal(t, int64(len(tt.expected)), written)
			assert.Equal(t, string(tt.expected), dst.String())
		})
	}
}

func TestCopyReturnsReadError(t *testing.T) {
	dst := &bytes.Buffer{}
	written, err := Copy(dst, &chunkReader{chunks: [][]byte{{'x'}}}, []byte{'A', 'B'})
	assert.ErrorIs(t, err, errUnexpectedRead)
	assert.Equal(t, int64(1), written)
	assert.Equal(t, "x", dst.String())
}

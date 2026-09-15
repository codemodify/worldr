package shell

import (
	"bytes"
	"testing"
)

func TestWireHostClipboardNil(t *testing.T) {
	var buf bytes.Buffer
	wireHostClipboard(nil, nil, &buf)
	if buf.Len() != 0 {
		t.Fatal(buf.String())
	}
}

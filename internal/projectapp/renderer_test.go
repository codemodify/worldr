package projectapp

import "testing"

func TestPreviewPreservesSourceAndClipsTabsControlsAndUnicode(t *testing.T) {
	const source = "λ\ttext\r\n\x1b[31m\u202eevil\nlast"
	preview := makePreview(source)
	if preview.text != source || len(preview.starts) != 3 || preview.line(0) != "λ\ttext" || preview.line(2) != "last" || preview.line(3) != "" {
		t.Fatal("preview indexing modified source text or mishandled CRLF")
	}
	for _, test := range []struct {
		line         string
		first, width int
		want         string
	}{
		{"λ\ttext", 0, 8, "λ   text"},
		{"λ\ttext", 2, 4, "  te"},
		{"λ\ttext", 4, 4, "text"},
		{"a\x1bb\u202ec", 0, 10, "a�b�c"},
		{"short", 20, 2, ""},
	} {
		if got := visibleLine(test.line, test.first, test.width); got != test.want {
			t.Errorf("visibleLine(%q, %d, %d)=%q; want %q", test.line, test.first, test.width, got, test.want)
		}
	}
	if got := safeLabel("untrusted\nname\u202e.txt"); got != `untrusted\u000aname\u202e.txt` {
		t.Fatalf("filename controls were not escaped: %q", got)
	}
}

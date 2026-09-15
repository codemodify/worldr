package clipbridge

import (
	"os"
	"syscall"
	"testing"
)

func TestIsPlainText(t *testing.T) {
	if !IsPlainText("text/plain") || !IsPlainText("text/plain;charset=utf-8") {
		t.Fatal("core")
	}
	if !IsPlainText("TEXT/PLAIN") || !IsPlainText("UTF8_STRING") {
		t.Fatal("alias")
	}
	if IsPlainText("image/png") || IsPlainText("") {
		t.Fatal("reject")
	}
}

func TestPickPlainMime(t *testing.T) {
	if PickPlainMime(nil) != "" {
		t.Fatal("empty")
	}
	if PickPlainMime([]string{"image/png", "text/plain"}) != MimeTextPlain {
		t.Fatal("prefer text/plain")
	}
	if PickPlainMime([]string{"text/plain;charset=utf-8"}) != MimeTextUTF8 {
		t.Fatal("utf8")
	}
	if PickPlainMime([]string{"image/png"}) != "" {
		t.Fatal("no text")
	}
}

func TestPickImageMime(t *testing.T) {
	if PickImageMime(nil) != "" {
		t.Fatal("empty")
	}
	if PickImageMime([]string{"text/plain", "image/png"}) != MimePNG {
		t.Fatal("png")
	}
	if PickImageMime([]string{"image/bmp"}) != MimeBMP {
		t.Fatal("bmp")
	}
	if PickImageMime([]string{"image/jpeg", "image/webp"}) != MimeJPEG {
		t.Fatal("jpeg before webp")
	}
	if PickImageMime([]string{"image/jpg"}) != "image/jpg" {
		t.Fatal("jpg alias")
	}
	if PickImageMime([]string{"image/webp"}) != MimeWebP {
		t.Fatal("webp")
	}
	if PickImageMime([]string{"text/plain"}) != "" {
		t.Fatal("text only")
	}
	if !IsImage("image/png") || !IsImage("image/bmp") || !IsImage("image/jpeg") || !IsImage("image/webp") || IsImage("text/plain") {
		t.Fatal("IsImage")
	}
	if CanonicalImage("image/jpg") != MimeJPEG || CanonicalImage("image/x-bmp") != MimeBMP {
		t.Fatal("canonical")
	}
}

func TestHostOfferMimes(t *testing.T) {
	if !Bridgeable([]string{"image/png"}) || !Bridgeable([]string{"text/plain"}) {
		t.Fatal("bridgeable")
	}
	if Bridgeable([]string{"application/octet-stream"}) {
		t.Fatal("unknown")
	}
	got := HostOfferMimes([]string{"text/plain", "image/png"})
	hasText, hasPNG := false, false
	for _, m := range got {
		if m == MimeTextPlain {
			hasText = true
		}
		if m == MimePNG {
			hasPNG = true
		}
	}
	if !hasText || !hasPNG {
		t.Fatalf("%v", got)
	}
	got = HostOfferMimes([]string{"image/png"})
	if len(got) != 1 || got[0] != MimePNG {
		t.Fatalf("png only %v", got)
	}
	got = HostOfferMimes([]string{"image/jpeg", "image/webp"})
	if len(got) != 2 || got[0] != MimeJPEG || got[1] != MimeWebP {
		t.Fatalf("jpeg+webp %v", got)
	}
	if CapFor(MimePNG) != MaxImageBytes || CapFor(MimeTextPlain) != MaxTextBytes {
		t.Fatal("caps")
	}
}

func TestHostOwnEcho(t *testing.T) {
	var h HostOwn
	if h.Owns(false) || h.Owns(true) {
		t.Fatal("start")
	}
	h.Set(false, true)
	if !h.Owns(false) || h.Owns(true) {
		t.Fatal("clip only")
	}
	h.Set(false, false)
	h.Set(true, true)
	if h.Owns(false) || !h.Owns(true) {
		t.Fatal("prim only")
	}
}

func TestWriteText(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	fd, err := syscall.Dup(int(w.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	WriteText(fd, []byte("hello"))
	buf := make([]byte, 16)
	n, err := r.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("%q %v", buf[:n], err)
	}
}

func TestWriteTextNilFD(t *testing.T) {
	WriteText(0, []byte("x"))
	WriteText(-1, []byte("x"))
}

func TestWriteTextCapsSlice(t *testing.T) {
	big := make([]byte, MaxTextBytes+64)
	if len(big) <= MaxTextBytes {
		t.Fatal("setup")
	}
	// WriteText trims before write; use a file so a large write cannot block.
	f, err := os.CreateTemp(t.TempDir(), "clip")
	if err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	WriteText(fd, big)
	_ = f.Close()
	st, err := os.Stat(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != MaxTextBytes {
		t.Fatalf("got %d", st.Size())
	}
}

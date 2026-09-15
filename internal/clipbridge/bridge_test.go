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

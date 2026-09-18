package app

import (
	"io"
	"testing"
)

func TestNativeSDKApplicationOptionsAreBoundedAndRepeatable(t *testing.T) {
	o, err := Parse([]string{"--native-app=./instrument-a", "--native-app=instrument-b"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.NativeApplications) != 2 || o.NativeApplications[0] != "./instrument-a" || o.NativeApplications[1] != "instrument-b" {
		t.Fatalf("native app options: %+v", o.NativeApplications)
	}
	if _, err := Parse([]string{"--native-app="}, io.Discard); err == nil {
		t.Fatal("accepted an empty native application")
	}
	if _, err := Parse([]string{"--native-app=a", "--native-app=b", "--native-app=c", "--native-app=d", "--native-app=e"}, io.Discard); err == nil {
		t.Fatal("accepted more than four native applications")
	}
}

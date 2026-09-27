// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"
)

func TestTarFilesCreatesParentDirsFirst(t *testing.T) {
	archive, err := tarFiles(map[string][]byte{"/pki/sub/key": []byte("k"), "/pki/password": []byte("p")})
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(archive))
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	want := []string{"pki/", "pki/sub/", "pki/password", "pki/sub/key"}
	if len(names) != len(want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("entries = %v, want %v", names, want)
		}
	}
}

func TestTarFilesRejectsRelativePath(t *testing.T) {
	if _, err := tarFiles(map[string][]byte{"relative": nil}); err == nil {
		t.Fatal("relative path accepted")
	}
}

func TestFirstTarFileRoundTrip(t *testing.T) {
	archive, err := tarFiles(map[string][]byte{"/a/b": []byte("content")})
	if err != nil {
		t.Fatal(err)
	}
	got, err := firstTarFile(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "content" {
		t.Errorf("content = %q", got)
	}
}

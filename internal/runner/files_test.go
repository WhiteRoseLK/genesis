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
		t.Fatalf("entrées = %v, attendu %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("entrées = %v, attendu %v", names, want)
		}
	}
}

func TestTarFilesRejectsRelativePath(t *testing.T) {
	if _, err := tarFiles(map[string][]byte{"relatif": nil}); err == nil {
		t.Fatal("chemin relatif accepté")
	}
}

func TestFirstTarFileRoundTrip(t *testing.T) {
	archive, err := tarFiles(map[string][]byte{"/a/b": []byte("contenu")})
	if err != nil {
		t.Fatal(err)
	}
	got, err := firstTarFile(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "contenu" {
		t.Errorf("contenu = %q", got)
	}
}

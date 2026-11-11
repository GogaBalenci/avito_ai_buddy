package content

import (
	"io/fs"
	"testing"
)

func TestFindBannedName(t *testing.T) {
	if got := FindBannedName("Книга: Донован, Керниган"); got != "донован" {
		t.Fatalf("got %q", got)
	}
	if got := FindBannedName("Изучи принципы разработки"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestEmbeddedContentHasNoRealNames(t *testing.T) {
	err := fs.WalkDir(FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(FS(), p)
		if err != nil {
			return err
		}
		if n := FindBannedName(string(b)); n != "" {
			t.Errorf("%s contains a real person's name %q", p, n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

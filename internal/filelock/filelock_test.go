package filelock

import (
	"os"
	"testing"
)

func TestLockCanBeReused(t *testing.T) {
	file, err := Open(t.TempDir()+string(os.PathSeparator)+"lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := Lock(file, true, true); err != nil {
		t.Fatal(err)
	}
	if err := Unlock(file); err != nil {
		t.Fatal(err)
	}
	if err := Lock(file, true, true); err != nil {
		t.Fatal(err)
	}
	if err := Unlock(file); err != nil {
		t.Fatal(err)
	}
}

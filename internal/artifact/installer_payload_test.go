package artifact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/config"
)

func TestBuildInstallerPayload_FormsEmbedSixTypes(t *testing.T) {
	tmpSrc := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpSrc, "app.txt"), []byte("hello"), 0644)
	outDir := t.TempDir()
	forms := config.InstallerForms{
		"superAdmin": {
			Fields: []config.FormField{
				{Name: "username", Type: "text"},
				{Name: "email", Type: "email"},
				{Name: "password", Type: "password"},
				{Name: "role", Type: "select", Options: []string{"admin"}},
				{Name: "age", Type: "number"},
				{Name: "bio", Type: "textarea"},
			},
		},
	}
	b, _ := config.MarshalFormsJSON(forms)
	res, err := BuildInstallerPayload(InstallerPayloadOptions{
		SourceDir: tmpSrc,
		OutputDir: outDir,
		Version:   "1.2.3",
		Source:    "testproj",
		ProjectID: "testproj",
		FormsJSON: b,
	})
	if err != nil {
		t.Fatalf("BuildInstallerPayload: %v", err)
	}
	if !res.FormsEmbedded {
		t.Fatalf("FormsEmbedded should be true")
	}
	data, err := ReadFormsFromArtifact(res.BundlePath)
	if err != nil {
		t.Fatalf("ReadFormsFromArtifact: %v", err)
	}
	var decoded config.InstallerForms
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded["superAdmin"].Fields) != 6 {
		t.Fatalf("want 6 fields got %d", len(decoded["superAdmin"].Fields))
	}
	types := map[string]bool{}
	for _, f := range decoded["superAdmin"].Fields {
		types[f.Type] = true
	}
	for _, want := range []string{"text", "email", "password", "select", "number", "textarea"} {
		if !types[want] {
			t.Fatalf("missing type %q", want)
		}
	}
}

func TestBuildInstallerPayload_ReuseSkipPackage(t *testing.T) {
	tmpSrc := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpSrc, "a.txt"), []byte("content v1"), 0644)
	outDir := t.TempDir()
	// first build artifact directly
	pkgRes, err := Package(PackageOptions{
		SourceDir: tmpSrc,
		OutputDir: outDir,
		Version:   "1.0.0",
		Source:    "proj",
		ProjectID: "proj",
		Formats:   []string{"tar.gz"},
	})
	if err != nil {
		t.Fatalf("Package: %v", err)
	}
	// now reuse via BuildInstallerPayload with --artifact
	forms := config.InstallerForms{"f": {Fields: []config.FormField{{Name: "email", Type: "email"}}}}
	b, _ := config.MarshalFormsJSON(forms)
	reuseOut := t.TempDir()
	res, err := BuildInstallerPayload(InstallerPayloadOptions{
		OutputDir:     reuseOut,
		Version:       "1.0.0",
		Source:        "proj",
		ProjectID:     "proj",
		ReuseArtifact: pkgRes.ArtifactPath,
		FormsJSON:     b,
	})
	if err != nil {
		t.Fatalf("reuse BuildInstallerPayload: %v", err)
	}
	if res.BundlePath == pkgRes.ArtifactPath {
		t.Fatalf("reuse should create new bundle with forms.json, not same path")
	}
	if !res.FormsEmbedded {
		t.Fatalf("forms should be embedded on reuse")
	}
	// verify bundle still passes verification
	vr, err := VerifyArtifact(res.BundlePath)
	if err != nil || !vr.Passed {
		t.Fatalf("bundle verify failed: %v passed=%v", err, vr.Passed)
	}
	// verify tampered artifact would fail reuse
	tampered := filepath.Join(t.TempDir(), "tampered.tar.gz")
	data, _ := os.ReadFile(pkgRes.ArtifactPath)
	if len(data) > 100 {
		data[100] ^= 0xFF
		_ = os.WriteFile(tampered, data, 0644)
		_, err = BuildInstallerPayload(InstallerPayloadOptions{
			OutputDir:     t.TempDir(),
			ReuseArtifact: tampered,
			FormsJSON:     b,
		})
		if err == nil {
			t.Fatalf("tampered reuse should fail")
		}
	}
}

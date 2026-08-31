package artifact

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FormsPayloadFile is the well-known path of forms.json inside the artifact/installer payload.
const FormsPayloadFile = "forms.json"

// InstallerPayloadOptions configures installer payload bundling (artifact + manifest + checksum + forms.json).
type InstallerPayloadOptions struct {
	ArtifactPath  string // verified artifact tar.gz path (when reusing)
	SourceDir     string // source dir when building artifact
	OutputDir     string
	Version       string
	Source        string
	ProjectID     string
	Include       []string
	Exclude       []string
	FormsJSON     []byte // marshaled forms.json, nil means no forms
	ReuseArtifact string // if set, path to existing artifact to reuse (verify PASS -> skip Package)
}

// InstallerPayloadResult describes bundling outcome.
type InstallerPayloadResult struct {
	BundlePath    string
	ArtifactPath  string
	FormsEmbedded bool
	Manifest      *Manifest
}

// BuildInstallerPayload builds or reuses artifact, then bundles with forms.json into installer payload.
// If opts.ReuseArtifact != "" and VerifyArtifact passes, Package is skipped (avoid double build).
func BuildInstallerPayload(opts InstallerPayloadOptions) (*InstallerPayloadResult, error) {
	var pkgRes *PackageResult
	var artifactPath string

	// Reuse path if provided and verifies
	if opts.ReuseArtifact != "" {
		vr, err := VerifyArtifact(opts.ReuseArtifact)
		if err != nil {
			return nil, fmt.Errorf("verify reused artifact: %w", err)
		}
		if !vr.Passed {
			return nil, fmt.Errorf("reused artifact verification failed")
		}
		artifactPath = opts.ReuseArtifact
		manifest, err := ReadManifest(artifactPath)
		if err != nil {
			// try metadata fallback
			manifest = &Manifest{ArtifactID: "reused", Version: opts.Version, Source: opts.Source, ProjectID: opts.ProjectID}
		}
		pkgRes = &PackageResult{ArtifactPath: artifactPath, Manifest: manifest}
	} else if opts.ArtifactPath != "" {
		vr, err := VerifyArtifact(opts.ArtifactPath)
		if err != nil {
			return nil, fmt.Errorf("verify artifactPath: %w", err)
		}
		if !vr.Passed {
			return nil, fmt.Errorf("artifactPath verification failed")
		}
		artifactPath = opts.ArtifactPath
		m, _ := ReadManifest(artifactPath)
		pkgRes = &PackageResult{ArtifactPath: artifactPath, Manifest: m}
	} else {
		// Fresh package
		var err error
		pkgRes, err = Package(PackageOptions{
			SourceDir: opts.SourceDir,
			OutputDir: opts.OutputDir,
			Version:   opts.Version,
			Source:    opts.Source,
			ProjectID: opts.ProjectID,
			Include:   opts.Include,
			Exclude:   opts.Exclude,
			Formats:   []string{"tar.gz"},
		})
		if err != nil {
			return nil, err
		}
		artifactPath = pkgRes.ArtifactPath
		vr, err := VerifyArtifact(artifactPath)
		if err != nil || !vr.Passed {
			return nil, fmt.Errorf("fresh artifact verification failed: %v", err)
		}
	}

	// Forms JSON to embed: if provided, create bundle that contains artifact + forms.json
	// For MVP, installer payload is the artifact itself with forms.json injected alongside manifest.
	// We inject forms.json into the artifact tar if not already present.
	if opts.FormsJSON != nil && len(opts.FormsJSON) > 0 {
		bundlePath, err := injectFormsIntoArtifact(artifactPath, opts.FormsJSON, opts.OutputDir)
		if err != nil {
			return nil, fmt.Errorf("embed forms.json: %w", err)
		}
		return &InstallerPayloadResult{
			BundlePath:    bundlePath,
			ArtifactPath:  bundlePath,
			FormsEmbedded: true,
			Manifest:      pkgRes.Manifest,
		}, nil
	}

	return &InstallerPayloadResult{
		BundlePath:    artifactPath,
		ArtifactPath:  artifactPath,
		FormsEmbedded: false,
		Manifest:      pkgRes.Manifest,
	}, nil
}

// injectFormsIntoArtifact creates a new tar.gz that copies existing artifact content plus forms.json.
func injectFormsIntoArtifact(srcPath string, formsJSON []byte, outputDir string) (string, error) {
	// Validate formsJSON is valid JSON
	var tmp json.RawMessage
	if err := json.Unmarshal(formsJSON, &tmp); err != nil {
		return "", fmt.Errorf("formsJSON invalid JSON: %w", err)
	}
	if outputDir == "" {
		outputDir = os.TempDir()
	}
	_ = os.MkdirAll(outputDir, 0755)
	dstPath := filepath.Join(outputDir, "bundle-"+filepath.Base(srcPath))

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer srcFile.Close()

	gzr, err := gzip.NewReader(srcFile)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return "", err
	}

	gzw := gzip.NewWriter(dstFile)
	tw := tar.NewWriter(gzw)

	hasForms := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Name == FormsPayloadFile {
			hasForms = true
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return "", err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return "", err
		}
	}
	if !hasForms {
		hdr := &tar.Header{
			Name:     FormsPayloadFile,
			Size:     int64(len(formsJSON)),
			Mode:     0644,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return "", err
		}
		if _, err := tw.Write(formsJSON); err != nil {
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		gzw.Close()
		dstFile.Close()
		return "", err
	}
	if err := gzw.Close(); err != nil {
		dstFile.Close()
		return "", err
	}
	if err := dstFile.Close(); err != nil {
		return "", err
	}
	// reopen to ensure valid gzip
	if _, err := os.Stat(dstPath); err != nil {
		return "", err
	}
	return dstPath, nil
}

// ReadFormsFromArtifact extracts forms.json from artifact if present.
func ReadFormsFromArtifact(artifactPath string) ([]byte, error) {
	f, err := os.Open(artifactPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == FormsPayloadFile {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("forms.json not found in artifact")
}

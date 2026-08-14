// Command skillpack is the standard-skills release packing step
// (ST-021-03; ADR-037 D2): it packs a standard repo's authored skill
// content into the per-skill release assets and emits the release-metadata
// fragment the standard repo merges into its registry metadata document
// before signing.
//
// It is a release-pipeline tool, not a user command — the standard repos'
// release workflows run it (or vendor internal/skillpack), mirroring the
// cmd/release-sign convention of the standard repositories. It never
// holds a signing key: attestation is the standard's own release-sign
// step, applied AFTER this step merges the fragment.
//
// Usage:
//
//	skillpack --standard <standard-id> --content <dir> --out <dir>
//
// The --content directory follows the packer layout: skills.json plus one
// directory per skill (<name>/SKILL.md + optional extra files). The step
// writes:
//
//	<out>/assets/<asset-id>     the bundle archive bytes, file named the
//	                            metadata asset identifier (the release
//	                            channel file the install gate fetches)
//	<out>/skills-metadata.json  {"skills": [...], "trust":
//	                            {"contentDigests": [...]}} — merge into the
//	                            release metadata BEFORE signing (skills at
//	                            the document root, digests under
//	                            trust.contentDigests); every asset is bound
//	                            to an attestation-bound named digest
//
// and prints one line per packed skill (asset id, sha-256, bytes) to
// stdout for the workflow log.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"maleolabs.com/anvil/internal/skillpack"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "skillpack: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("skillpack", flag.ExitOnError)
	standard := fs.String("standard", "", "the standard id the skills ship with (e.g. anvil-standard-laravel); becomes the bundle source and the provenance header source")
	content := fs.String("content", "", "path to the standard's skills content directory (contains skills.json and one directory per skill)")
	out := fs.String("out", "", "output directory; assets/ and skills-metadata.json are written here")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: skillpack --standard <id> --content <dir> --out <dir>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *standard == "" || *content == "" || *out == "" {
		fs.Usage()
		return fmt.Errorf("--standard, --content and --out are required")
	}

	packed, err := skillpack.PackStandard(*content, *standard)
	if err != nil {
		return err
	}

	assetsDir := filepath.Join(*out, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return fmt.Errorf("cannot create the assets directory %s: %w", assetsDir, err)
	}
	for _, s := range packed {
		if err := os.WriteFile(filepath.Join(assetsDir, s.AssetID), s.Bundle, 0o644); err != nil {
			return fmt.Errorf("cannot write the release asset %s: %w", s.AssetID, err)
		}
	}

	fragment := skillpack.BuildFragment(packed)
	raw, err := json.MarshalIndent(fragment, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the release-metadata fragment: %w", err)
	}
	raw = append(raw, '\n')
	fragmentPath := filepath.Join(*out, "skills-metadata.json")
	if err := os.WriteFile(fragmentPath, raw, 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", fragmentPath, err)
	}

	fmt.Printf("packed %d skill(s) for %s\n", len(packed), *standard)
	for _, s := range packed {
		fmt.Printf("  %s  sha256:%s  %d bytes\n", s.AssetID, s.SHA256Hex, len(s.Bundle))
	}
	fmt.Printf("fragment: %s (merge into the release metadata before signing)\n", fragmentPath)
	return nil
}

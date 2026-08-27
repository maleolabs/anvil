package main

import (
	"fmt"
	"os"

	spike "maleolabs.com/anvil/spikes/local-deploy-ssh-transport"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./spikes/local-deploy-ssh-transport/scripts/verify.go <artifact.tar.gz> [remote.tar.gz]")
		os.Exit(2)
	}
	art := os.Args[1]
	remote := ""
	if len(os.Args) > 2 {
		remote = os.Args[2]
	}
	if err := spike.VerifyChecksum(art); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("PASS: manifest checksum valid for %s\n", art)
	m, _, _ := spike.ExtractManifest(art)
	if m != nil {
		preview := m.Checksum
		if len(preview) > 16 {
			preview = preview[:16]
		}
		fmt.Printf("  artifact_id=%s checksum=%s (%s) project_id=%s\n", m.ArtifactID, preview, m.ChecksumType, m.ProjectID)
	}
	if remote != "" {
		if _, err := os.Stat(remote); err == nil {
			if err := spike.VerifyChecksum(remote); err != nil {
				fmt.Fprintf(os.Stderr, "FAIL remote: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("PASS: remote checksum valid for %s\n", remote)
			src, _ := spike.ChecksumFile(art)
			dst, _ := spike.ChecksumFile(remote)
			if src == dst && src != "" {
				fmt.Printf("PASS: file sha256 match %s\n", src[:16])
			} else {
				fmt.Printf("FAIL: sha256 mismatch src=%s dst=%s\n", src, dst)
				os.Exit(1)
			}
		}
	}
}

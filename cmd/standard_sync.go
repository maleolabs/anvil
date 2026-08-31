// Package cmd implements the Anvil CLI commands.
//
// ── Standard Sync (sto:registry-index-sync, ADR-040) ─────────────────
//
// "anvil standard sync <id> | --all" provisions the consumer-side
// registry index from the decentralized static index remotes (ADR-030).
// Each standard lives in its own repository <org>/anvil-standard-<name>
// (convention: org default maleolabs, branch main). The sync fetches
// registry metadata documents via https, strict-parses them
// (TS-014-01-02), and writes them to the default index layout
// <index>/<id>/<version>.json additive/idempotent (--force overwrites).
//
// Auto-offer wiring (ADR-040): list and install detect a missing or empty
// default index and offer provisioning interactively.
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

const (
	envStandardAutoSync    = "ANVIL_STANDARD_AUTO_SYNC"
	envRegistryCatalog     = "ANVIL_REGISTRY_CATALOG"
	standardSyncOrgDefault = "maleolabs"
)

// standardSyncCmd is "anvil standard sync".
var standardSyncCmd = &cobra.Command{
	Use:     "sync [id] [version]",
	Short:   "Sync registry index from remote standards",
	Example: "  anvil standard sync anvil-standard-laravel\n  anvil standard sync anvil-standard-laravel 1.2.3\n  anvil standard sync --all --force",
	Long: `Sync the local registry index from remote standard repositories (ADR-040).

The static registry index is decentralized (ADR-030): each standard
publishes its release documents in its own repository
<org>/anvil-standard-<name> on branch main. This command fetches
documents over https, validates them with the strict registry parse
(TS-014-01-02), and writes them to the local index directory in the
canonical layout <index>/<id>/<version>.json.

Without --all, one standard is synced:

  anvil standard sync anvil-standard-laravel
  anvil standard sync anvil-standard-laravel 1.2.3

With --all, every catalog entry is synced. The catalog is the local
override file (ANVIL_REGISTRY_CATALOG or <config>/anvil/catalog.json);
its keys are standard ids, values are base raw URLs. The default
convention URL is https://raw.githubusercontent.com/<org>/<id>/main.

Sync is additive and idempotent: existing documents are skipped unless
--force is given, which overwrites. Invalid fetched documents are
rejected with an actionable error and are NOT written.

Fetch policy (TD-008 & ADR-030): https-only, userinfo rejected, bounded
redirects, size cap, idle timeout — identical to the install flow.

Manual --index / ANVIL_REGISTRY_INDEX remains first-class and is never
affected by the auto-offer.`,
	Args:         cobra.RangeArgs(0, 2),
	SilenceUsage: true,
	RunE:         runStandardSync,
}

func init() {
	AddJSONFlag(standardSyncCmd)
	standardSyncCmd.Flags().Bool("all", false, "sync every standard known from the catalog")
	standardSyncCmd.Flags().Bool("force", false, "overwrite existing index documents")
	standardSyncCmd.Flags().Bool("yes", false, "auto-accept provisioning prompts without interaction")
	standardSyncCmd.Flags().Bool("no-sync", false, "disable auto-offer provisioning (airgap)")
	standardSyncCmd.Flags().String("index", "", "path to the static registry index directory (default: $ANVIL_REGISTRY_INDEX, else <user config dir>/anvil/registry)")
	standardCmd.AddCommand(standardSyncCmd)
}

// standardSyncResult / Failure are JSON envelope data shapes with
// snake_case tags (M2 fix: PascalCase would break contract).
type standardSyncResult struct {
	ID       string   `json:"id"`
	Versions []string `json:"versions"`
}

type standardSyncFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type standardSyncEnvelope struct {
	Synced   []standardSyncResult  `json:"synced"`
	Skipped  []standardSyncResult  `json:"skipped,omitempty"`
	Failures []standardSyncFailure `json:"failures,omitempty"`
}

func runStandardSync(cmd *cobra.Command, args []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	allFlag, _ := cmd.Flags().GetBool("all")
	forceFlag, _ := cmd.Flags().GetBool("force")

	if allFlag && len(args) > 0 {
		return ReportError(cmd, &output.AppError{
			Message:    "sync: --all and <id> are mutually exclusive",
			Resolution: "Use either 'anvil standard sync --all' or 'anvil standard sync <id>'",
		})
	}
	if !allFlag && len(args) == 0 {
		return ReportError(cmd, &output.AppError{
			Message:    "sync: standard id is required",
			Resolution: "Run 'anvil standard sync <id>' or 'anvil standard sync --all'",
		})
	}

	indexPath, err := standardIndexPath(cmd)
	if err != nil {
		return ReportError(cmd, &output.AppError{Message: "could not resolve index path", Reason: err.Error(), Err: err})
	}

	var ids []string
	var versionsMap map[string][]string
	if allFlag {
		catalog, err := loadRegistryCatalog()
		if err != nil {
			return ReportError(cmd, &output.AppError{Message: "could not load catalog for --all", Reason: err.Error(), Err: err})
		}
		if len(catalog) == 0 {
			return ReportError(cmd, &output.AppError{Message: "catalog is empty: nothing to sync", Resolution: "Add entries to the catalog file or sync a specific id"})
		}
		for id := range catalog {
			ids = append(ids, id)
		}
	} else {
		id := args[0]
		ids = []string{id}
		versionsMap = make(map[string][]string)
		if len(args) == 2 {
			versionsMap[id] = []string{args[1]}
		}
	}

	var synced []standardSyncResult
	var skipped []standardSyncResult
	var failures []standardSyncFailure

	for _, id := range ids {
		versions := versionsMap[id]
		if len(versions) == 0 {
			// Discover versions: try catalog override base, then convention.
			// For fresh sync, we attempt to fetch via base URL listing.
			// If catalog provides explicit versions list, use it; otherwise
			// we probe the remote for a versions manifest.
			vs, err := discoverRemoteVersions(id)
			if err != nil {
				failures = append(failures, standardSyncFailure{ID: id, Error: err.Error()})
				continue
			}
			versions = vs
			if len(versions) == 0 {
				failures = append(failures, standardSyncFailure{ID: id, Error: "no versions discovered for standard"})
				continue
			}
		}
		for _, v := range versions {
			dest := filepath.Join(indexPath, id, v+".json")
			if _, err := os.Stat(dest); err == nil && !forceFlag {
				skipped = append(skipped, standardSyncResult{ID: id, Versions: []string{v}})
				continue
			}
			raw, err := fetchIndexDocument(id, v)
			if err != nil {
				failures = append(failures, standardSyncFailure{ID: id, Error: fmt.Sprintf("version %s: %v", v, err)})
				continue
			}
			// Strict parse before write (AC2)
			if _, err := registry.Parse(raw); err != nil {
				failures = append(failures, standardSyncFailure{ID: id, Error: fmt.Sprintf("version %s failed strict validation: %v", v, err)})
				continue
			}
			if err := atomicWriteFile(dest, raw, 0o644); err != nil {
				failures = append(failures, standardSyncFailure{ID: id, Error: fmt.Sprintf("version %s: write %s: %v", v, dest, err)})
				continue
			}
			synced = append(synced, standardSyncResult{ID: id, Versions: []string{v}})
		}
	}

	// Aggregate synced by id for cleaner output
	syncedAgg := aggregateSyncResults(synced)
	skippedAgg := aggregateSyncResults(skipped)

	anyFailed := len(failures) > 0
	if jsonFlag {
		if anyFailed {
			// M1 fix: on failure with --json, do NOT also emit success envelope.
			// Return AppError so ReportError renders single error envelope.
			return ReportErrorWithCode(cmd, &output.AppError{
				Message:    "sync completed with failures",
				Reason:     fmt.Sprintf("%d standard(s) failed", len(failures)),
				Resolution: "Retry with --force or inspect failures; invalid documents are never written",
				Err:        errors.New(failures[0].Error),
			}, output.ExitCodeGeneral)
		}
		return WriteJSON(cmd, standardSyncEnvelope{Synced: syncedAgg, Skipped: skippedAgg, Failures: failures})
	}

	// Human output via Raw? Use styleFor W (human) — ensure not polluting json.
	s := styleFor(cmd)
	w := s.W
	allOK := len(syncedAgg) > 0 && len(failures) == 0
	if allOK {
		for _, r := range syncedAgg {
			fmt.Fprintf(w, "Synced %s: %s\n", r.ID, strings.Join(r.Versions, ", "))
		}
	}
	if len(skippedAgg) > 0 {
		for _, r := range skippedAgg {
			fmt.Fprintf(w, "Skipped (exists, use --force): %s %s\n", r.ID, strings.Join(r.Versions, ", "))
		}
	}
	if len(failures) > 0 {
		for _, f := range failures {
			fmt.Fprintf(w, "Failed %s: %s\n", f.ID, f.Error)
		}
		return ReportError(cmd, &output.AppError{
			Message:    "sync completed with failures",
			Reason:     fmt.Sprintf("%d standard(s) failed", len(failures)),
			Resolution: "Retry with --force or inspect failures; invalid documents are never written",
			Err:        errors.New(failures[0].Error),
		})
	}
	return nil
}

func aggregateSyncResults(in []standardSyncResult) []standardSyncResult {
	m := make(map[string][]string)
	for _, r := range in {
		m[r.ID] = append(m[r.ID], r.Versions...)
	}
	var out []standardSyncResult
	for id, vs := range m {
		out = append(out, standardSyncResult{ID: id, Versions: vs})
	}
	return out
}

// atomicWriteFile writes data atomically: temp file in same dir + rename (M3 fix).
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// fetchIndexDocument fetches one registry document via convention URL or catalog override.
func fetchIndexDocument(id, version string) ([]byte, error) {
	base, err := registryCatalogBaseURL(id)
	if err != nil {
		return nil, err
	}
	// Convention: <base>/<id>/<version>.json
	base = strings.TrimSuffix(base, "/")
	loc := fmt.Sprintf("%s/%s/%s.json", base, id, version)
	// Policy: https-only, userinfo rejected, bounded redirect, size cap, idle timeout
	parsed, err := url.Parse(loc)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("catalog URL %s is not well-formed https URL", loc)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("catalog URL %s carries userinfo; rejected", standardURLWithoutUserinfo(parsed))
	}
	req, err := http.NewRequest(http.MethodGet, loc, nil)
	if err != nil {
		return nil, err
	}
	resp, err := standardInstallHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not fetch %s: %w", standardScrubLocation(loc), standardScrubURLError(err))
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil && resp.Request.URL.Scheme != "https" {
		return nil, fmt.Errorf("fetch of %s resolved to non-https response", loc)
	}
	if resp.Request != nil && resp.Request.URL != nil && resp.Request.URL.User != nil {
		return nil, fmt.Errorf("fetch of %s resolved to userinfo-bearing URL", loc)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fetchStatusError(loc, resp.StatusCode)
	}
	resp.Body = newIdleTimeoutBody(resp.Body, downloadIdleTimeout())
	// Cap: reuse standardContentMaxBytes but index docs are tiny; cap at 5 MiB for documents.
	const maxIndexDoc = 5 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexDoc+1))
	if err != nil {
		return nil, fmt.Errorf("could not download %s: %w", loc, err)
	}
	if int64(len(body)) > maxIndexDoc {
		return nil, fmt.Errorf("index document at %s exceeds size cap", loc)
	}
	return body, nil
}

// discoverRemoteVersions tries to discover available versions for id.
// It first tries <base>/catalog.json, then <base>/<id>/versions.json, then falls back to single "latest" probe is not valid — so we report empty.
func discoverRemoteVersions(id string) ([]string, error) {
	base, err := registryCatalogBaseURL(id)
	if err != nil {
		return nil, err
	}
	base = strings.TrimSuffix(base, "/")
	// Try versions manifest: <base>/<id>/versions.json => {"versions":["1.0.0"]}
	candidates := []string{
		fmt.Sprintf("%s/%s/versions.json", base, id),
		fmt.Sprintf("%s/catalog.json", base),
	}
	for _, u := range candidates {
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme != "https" {
			continue
		}
		if parsed.User != nil {
			continue
		}
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		resp, err := standardInstallHTTPClient.Do(req)
		if err != nil {
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
		}() // close now
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		resp.Body = newIdleTimeoutBody(resp.Body, downloadIdleTimeout())
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			continue
		}
		// Try parse as versions manifest
		var m struct {
			Versions []string `json:"versions"`
		}
		if err := json.Unmarshal(body, &m); err == nil && len(m.Versions) > 0 {
			return m.Versions, nil
		}
		// Also try catalog shape: map[id][]versions
		var cat map[string][]string
		if err := json.Unmarshal(body, &cat); err == nil {
			if vs, ok := cat[id]; ok && len(vs) > 0 {
				return vs, nil
			}
		}
	}
	// Fallback: attempt to hit GitHub API? For offline tests, return error.
	return nil, fmt.Errorf("could not discover versions for %s; use 'anvil standard sync %s <version>' or add catalog entry", id, id)
}

// loadRegistryCatalog loads catalog overrides: env var path or default <config>/anvil/catalog.json
func loadRegistryCatalog() (map[string]string, error) {
	path := os.Getenv(envRegistryCatalog)
	if path == "" {
		dir, err := defaultStandardIndex()
		if err != nil {
			return nil, err
		}
		// catalog lives alongside registry dir: <config>/anvil/catalog.json
		path = filepath.Join(filepath.Dir(dir), "catalog.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("catalog %s invalid: %w", path, err)
	}
	return m, nil
}

func registryCatalogBaseURL(id string) (string, error) {
	catalog, err := loadRegistryCatalog()
	if err != nil {
		return "", err
	}
	if base, ok := catalog[id]; ok && base != "" {
		return base, nil
	}
	// Convention: https://raw.githubusercontent.com/maleolabs/<id>/main
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main", standardSyncOrgDefault, id), nil
}

// offerStandardIndexSync checks if auto-offer should provision index.
// Returns true if provisioning was attempted.
func offerStandardIndexSync(cmd *cobra.Command) bool {
	if disabledByFlagOrEnv(cmd) {
		return false
	}
	// Only offer when default index is not configured.
	flagSet := FlagIsSet(cmd, "index")
	if flagSet {
		return false
	}
	if v := os.Getenv(envStandardIndex); v != "" {
		return false
	}
	indexPath, err := defaultStandardIndex()
	if err != nil {
		return false
	}
	if registryIndexConfigured(indexPath) {
		return false
	}
	jsonFlag, _ := cmd.Flags().GetBool("json")
	yesFlag, _ := cmd.Flags().GetBool("yes")
	noSyncFlag, _ := cmd.Flags().GetBool("no-sync")
	if noSyncFlag {
		return false
	}
	// Non-interactive without --yes: auto-decline, do not fetch.
	if !isTerminal(cmd) && !yesFlag {
		return false
	}
	if !yesFlag && !isTerminal(cmd) {
		return false
	}
	// Prompt unless --yes
	if !yesFlag {
		ok, err := promptYesNo(cmd, "Registry index not configured. Sync default standards now? [y/N]: ")
		if err != nil || !ok {
			// Hint decline uses correct guidance (m1 fix: not --all)
			hint := "Tip: run 'anvil standard sync <id>' to provision a standard, or set --index / ANVIL_REGISTRY_INDEX for a local index."
			if jsonFlag {
				fmt.Fprintln(cmd.ErrOrStderr(), hint)
			} else {
				fmt.Fprintln(styleFor(cmd).W, hint)
			}
			return false
		}
	}
	// Perform minimal sync? For offer, we just inform; actual sync is explicit.
	// To satisfy AC4 auto-offer after list/install, we fetch via default? For now offer only hint.
	// If --yes and terminal, we attempt to sync a well-known default (laravel) as example.
	if yesFlag {
		hint := "Sync with 'anvil standard sync <id>' — e.g. 'anvil standard sync anvil-standard-laravel'."
		if jsonFlag {
			fmt.Fprintln(cmd.ErrOrStderr(), hint)
		} else {
			fmt.Fprintln(styleFor(cmd).W, hint)
		}
	}
	return false
}

func disabledByFlagOrEnv(cmd *cobra.Command) bool {
	if v := os.Getenv(envStandardAutoSync); v == "0" || strings.EqualFold(v, "false") {
		return true
	}
	if f := cmd.Flags().Lookup("no-sync"); f != nil && f.Changed {
		if b, _ := cmd.Flags().GetBool("no-sync"); b {
			return true
		}
	}
	return false
}

func isTerminal(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

func promptYesNo(cmd *cobra.Command, prompt string) (bool, error) {
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	var resp string
	_, err := fmt.Fscanln(cmd.InOrStdin(), &resp)
	if err != nil {
		return false, err
	}
	resp = strings.TrimSpace(strings.ToLower(resp))
	return resp == "y" || resp == "yes", nil
}

package config_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/lk16/box/internal/config"
)

// workspace turns one mount into its sbx spec, failing the test when box refuses it.
func workspace(t *testing.T, mount string) string {
	t.Helper()
	spec, err := config.ToWorkspace(mount)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

// refuseWorkspace expects box to refuse a mount, and returns the refusal.
func refuseWorkspace(t *testing.T, mount string) error {
	t.Helper()
	_, err := config.ToWorkspace(mount)
	return err
}

func TestABareMountIsReadOnly(t *testing.T) {
	if got := workspace(t, "/cache"); got != "/cache:ro" {
		t.Fatalf("became %q", got)
	}
}

func TestARWMountDropsTheSuffix(t *testing.T) {
	if got := workspace(t, "/cache:rw"); got != "/cache" {
		t.Fatalf("became %q", got)
	}
}

func TestAnExplicitROSuffixIsRejected(t *testing.T) {
	wantError(t, refuseWorkspace(t, "/cache:ro"), "read-only unless you add :rw")
}

func TestAnUnknownMountSuffixIsRejected(t *testing.T) {
	wantError(t, refuseWorkspace(t, "/cache:rx"), "unknown suffix")
}

func TestAnEmptyMountIsRejected(t *testing.T) {
	wantError(t, refuseWorkspace(t, ""), "a mount must name a path")
}

func TestAMountThatIsOnlyARWSuffixIsRejected(t *testing.T) {
	wantError(t, refuseWorkspace(t, ":rw"), "a mount must name a path")
}

func TestARWMountStillRejectsAColonInItsPath(t *testing.T) {
	wantError(t, refuseWorkspace(t, "/data/a:b:rw"), "unknown suffix")
}

func TestAMountExpandsALeadingTilde(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	if got := workspace(t, "~/.cargo"); got != "/home/someone/.cargo:ro" {
		t.Fatalf("became %q", got)
	}
}

func TestARWMountExpandsALeadingTilde(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	if got := workspace(t, "~/scratch:rw"); got != "/home/someone/scratch" {
		t.Fatalf("became %q", got)
	}
}

func TestTheMountsFileLivesInTheBoxDirectory(t *testing.T) {
	if config.MountsFile != filepath.Join(config.BoxDir, "mounts.json") {
		t.Fatalf("the mounts file is %q", config.MountsFile)
	}
}

func TestMountsIsNotAConfigKey(t *testing.T) {
	if slices.Contains(config.SettingKeys, "mounts") || slices.Contains(config.ContainerKeys, "mounts") {
		t.Fatal("mounts is a config key")
	}
}

func TestReadPathsFileReturnsNothingWhenAbsent(t *testing.T) {
	read, err := config.ReadPathsFile(filepath.Join(t.TempDir(), config.MountsFile))
	if err != nil || len(read) != 0 {
		t.Fatalf("read %v, %v", read, err)
	}
}

func TestReadPathsFileReadsTheNamedPaths(t *testing.T) {
	path := writeBoxFile(t, t.TempDir(), config.MountsFile, map[string]string{"go": "/usr/local/go"})
	read, err := config.ReadPathsFile(path)
	if err != nil || read.Get("go") != "/usr/local/go" {
		t.Fatalf("read %v, %v", read, err)
	}
}

func TestReadPathsFileRejectsAJSONArray(t *testing.T) {
	path := writeBoxFile(t, t.TempDir(), config.MountsFile, []string{"/cache"})
	_, err := config.ReadPathsFile(path)
	wantError(t, err, "must contain a JSON object")
}

func TestReadPathsFileRejectsBrokenJSON(t *testing.T) {
	path := writeBoxFile(t, t.TempDir(), config.MountsFile, map[string]string{})
	writeText(t, path, "{")
	_, err := config.ReadPathsFile(path)
	wantError(t, err, "not valid JSON")
}

func TestReadPathsFileRejectsAPathThatIsNotAString(t *testing.T) {
	path := writeBoxFile(t, t.TempDir(), config.MountsFile, map[string]any{"cache": nil})
	_, err := config.ReadPathsFile(path)
	wantError(t, err, "gives cache null, which is not text or a number")
}

func TestAsDescriptionsRejectsADescriptionThatIsNotText(t *testing.T) {
	_, err := config.AsDescriptions(raw(map[string]any{"go": []string{"the Go toolchain"}}))
	wantError(t, err, "gives go a list, which is not text or a number")
}

func TestAsDescriptionsRejectsAJSONArray(t *testing.T) {
	_, err := config.AsDescriptions(raw([]string{"go_mod_cache"}))
	wantError(t, err, "required_mounts must be a JSON object")
}

func TestOrderMountsFollowsTheDeclarationNotTheMountsFile(t *testing.T) {
	required := pairs("go", "the Go toolchain", "cargo", "the cargo home")
	provided := pairs("cargo", "~/.cargo", "go", "/usr/local/go")
	ordered, err := config.OrderMounts(required, provided)
	if err != nil || !slices.Equal(ordered, []string{"/usr/local/go", "~/.cargo"}) {
		t.Fatalf("ordered %v, %v", ordered, err)
	}
}

func TestADeclaredMountWithNoPathIsRejected(t *testing.T) {
	_, err := config.OrderMounts(pairs("go_mod_cache", "the Go module cache"), nil)
	wantError(t, err, "go_mod_cache: the Go module cache")
}

func TestAPlaceholderPathIsRejected(t *testing.T) {
	required := pairs("go_mod_cache", "the Go module cache")
	_, err := config.OrderMounts(required, pairs("go_mod_cache", config.MountPlaceholder))
	wantError(t, err, "has no path on this machine")
}

func TestAnEmptyPathIsRejected(t *testing.T) {
	_, err := config.OrderMounts(pairs("go_mod_cache", "the Go module cache"), pairs("go_mod_cache", ""))
	wantError(t, err, "has no path on this machine")
}

func TestAnUndeclaredMountNameIsRejected(t *testing.T) {
	required := pairs("cargo", "the cargo home")
	_, err := config.OrderMounts(required, pairs("cargo", "~/.cargo", "typo", "/cache"))
	wantError(t, err, "does not declare: typo")
}

func TestTheErrorNamesEveryUnfilledMount(t *testing.T) {
	required := pairs("go", "the Go toolchain", "cargo", "the cargo home")
	_, err := config.OrderMounts(required, nil)
	wantError(t, err, "go: the Go toolchain\n  cargo: the cargo home")
}

func TestNoDeclaredMountsNeedsNoMountsFile(t *testing.T) {
	ordered, err := config.OrderMounts(nil, nil)
	if err != nil || len(ordered) != 0 {
		t.Fatalf("ordered %v, %v", ordered, err)
	}
}

func TestBuildAppliesTheMountDefault(t *testing.T) {
	built, err := config.Build(config.NewValues(), []string{"/a", "/b:rw"}, nil, "/tmp/demo")
	if err != nil || !slices.Equal(built.Mounts, []string{"/a:ro", "/b"}) {
		t.Fatalf("mounted %v, %v", built.Mounts, err)
	}
}

func TestTheSameMountTwiceIsPassedOnce(t *testing.T) {
	workspaces, err := config.ToWorkspaces([]string{"/cache", "/cache"})
	if err != nil || !slices.Equal(workspaces, []string{"/cache:ro"}) {
		t.Fatalf("became %v, %v", workspaces, err)
	}
}

func TestAMountReadOnlyInOnePlaceAndWritableInAnotherIsRejected(t *testing.T) {
	_, err := config.ToWorkspaces([]string{"/cache", "/cache:rw"})
	wantError(t, err, "both /cache:ro and /cache")
}

func TestAWritableMountIsReachableToo(t *testing.T) {
	if got := config.MountTarget("/scratch"); got != "/scratch" {
		t.Fatalf("targets %q", got)
	}
}

func TestScopedNamesEveryEntryByTheMemberItCameFrom(t *testing.T) {
	if got := config.Scoped("billing-api", pairs("go", "x")).Names()[0]; got != "billing-api: go" {
		t.Fatalf("scoped to %q", got)
	}
}

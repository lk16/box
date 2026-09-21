package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/config"
)

// gitlab is the one declared secret the tests use, and the host its value may go to.
var gitlab = config.Secret{Name: "GITLAB_TOKEN", Host: "gitlab.com"}

// writeSecretsFile writes a file of NAME=value lines beside the repository, and returns where it landed.
func writeSecretsFile(t *testing.T, directory, contents string) string {
	t.Helper()
	outside := directory + "-secrets"
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outside, "box.env")
	writeText(t, path, contents)
	return path
}

func TestReadConfigFileKeepsSecretHostsAnObject(t *testing.T) {
	declared := map[string]string{"GITLAB_TOKEN": "gitlab.com"}
	path := writeConfig(t, t.TempDir(), map[string]any{config.SecretHosts: declared})
	read, err := config.ReadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := config.ToSecrets(read.Container(config.SecretHosts))
	if err != nil || !slices.Equal(secrets, []config.Secret{gitlab}) {
		t.Fatalf("read %v, %v", secrets, err)
	}
}

func TestADeclaredSecretNamesAVariableAndItsOneHost(t *testing.T) {
	secrets, err := config.ToSecrets(raw(map[string]string{"GITLAB_TOKEN": "gitlab.com"}))
	if err != nil || !slices.Equal(secrets, []config.Secret{gitlab}) {
		t.Fatalf("read %v, %v", secrets, err)
	}
}

func TestNoDeclaredSecretsIsNoSecrets(t *testing.T) {
	secrets, err := config.ToSecrets(raw(map[string]string{}))
	if err != nil || len(secrets) != 0 {
		t.Fatalf("read %v, %v", secrets, err)
	}
}

func TestASecretNameThatIsNoVariableNameIsRejected(t *testing.T) {
	_, err := config.ToSecrets(raw(map[string]string{"gitlab token": "gitlab.com"}))
	wantError(t, err, "not a valid environment variable name")
}

func TestASecretWithoutAHostIsRejected(t *testing.T) {
	_, err := config.ToSecrets(raw(map[string]string{"GITLAB_TOKEN": ""}))
	wantError(t, err, "no host")
}

func TestASecretThatWouldTakeBoxOwnTokenVariableIsRejected(t *testing.T) {
	_, err := config.ToSecrets(raw(map[string]string{config.SecretEnv: "gitlab.com"}))
	wantError(t, err, "box's own token")
}

func TestASecretOnBoxOwnTokenHostIsRejected(t *testing.T) {
	_, err := config.ToSecrets(raw(map[string]string{"GITLAB_TOKEN": config.SecretHost}))
	wantError(t, err, "box's own token")
}

func TestSecretHostsMustBeAnObject(t *testing.T) {
	_, err := config.ToSecrets(raw([]string{"GITLAB_TOKEN"}))
	wantError(t, err, "must be a JSON object")
}

func TestParseSecretsFileReadsANameAndItsValue(t *testing.T) {
	read, err := config.ParseSecretsFile("box.env", "GITLAB_TOKEN=glpat-abc\n")
	if err != nil || read.Get("GITLAB_TOKEN") != "glpat-abc" {
		t.Fatalf("read %v, %v", read, err)
	}
}

func TestParseSecretsFileKeepsEverythingAfterTheFirstEquals(t *testing.T) {
	read, err := config.ParseSecretsFile("box.env", "A=x=y z\n")
	if err != nil || read.Get("A") != "x=y z" {
		t.Fatalf("read %v, %v", read, err)
	}
}

func TestParseSecretsFileSkipsBlankLinesAndComments(t *testing.T) {
	read, err := config.ParseSecretsFile("box.env", "# a comment\n\n   \nA=1\n")
	if err != nil || len(read) != 1 || read.Get("A") != "1" {
		t.Fatalf("read %v, %v", read, err)
	}
}

func TestParseSecretsFileRejectsALineThatIsNotAPair(t *testing.T) {
	_, err := config.ParseSecretsFile("box.env", "A=1\nGITLAB_TOKEN\n")
	wantError(t, err, "line 2 is not NAME=value")
}

func TestParseSecretsFileRejectsAnExportLine(t *testing.T) {
	_, err := config.ParseSecretsFile("box.env", "export A=1\n")
	wantError(t, err, "line 1 does not start with a variable name")
}

func TestParseSecretsFileRejectsSpacesAroundTheEquals(t *testing.T) {
	_, err := config.ParseSecretsFile("box.env", "A = 1\n")
	wantError(t, err, "line 1 does not start with a variable name")
}

func TestParseSecretsFileRejectsAQuotedValue(t *testing.T) {
	_, err := config.ParseSecretsFile("box.env", "A='glpat-abc'\n")
	wantError(t, err, "line 1 quotes its value")
}

func TestARejectedLineNeverCarriesTheValueItHolds(t *testing.T) {
	_, err := config.ParseSecretsFile("box.env", "A=\"glpat-abc\"\n")
	if err == nil || strings.Contains(err.Error(), "glpat-abc") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestAValueHoldingAQuoteIsKept(t *testing.T) {
	read, err := config.ParseSecretsFile("box.env", "A=gl'pat\n")
	if err != nil || read.Get("A") != "gl'pat" {
		t.Fatalf("read %v, %v", read, err)
	}
}

func TestReadSecretValuesNeedsNoFileWithoutADeclaration(t *testing.T) {
	stored, err := config.ReadSecretValues(nil, "")
	if err != nil || len(stored) != 0 {
		t.Fatalf("read %v, %v", stored, err)
	}
}

func TestReadSecretValuesGivesEveryDeclaredSecretItsValue(t *testing.T) {
	path := writeSecretsFile(t, t.TempDir(), "GITLAB_TOKEN=glpat-abc\nOTHER=ignored\n")
	stored, err := config.ReadSecretValues([]config.Secret{gitlab}, path)
	want := []config.SecretValue{{Secret: gitlab, Value: "glpat-abc"}}
	if err != nil || !slices.Equal(stored, want) {
		t.Fatalf("read %v, %v", stored, err)
	}
}

func TestReadSecretValuesSaysHowToSetTheFileUpWhenItIsUnset(t *testing.T) {
	_, err := config.ReadSecretValues([]config.Secret{gitlab}, "")
	wantError(t, err, config.SecretsEnv)
}

func TestReadSecretValuesRejectsAFileThatIsNotThere(t *testing.T) {
	_, err := config.ReadSecretValues([]config.Secret{gitlab}, filepath.Join(t.TempDir(), "nothing.env"))
	wantError(t, err, "does not exist")
}

func TestReadSecretValuesRejectsADeclaredNameTheFileLacks(t *testing.T) {
	path := writeSecretsFile(t, t.TempDir(), "OTHER=1\n")
	_, err := config.ReadSecretValues([]config.Secret{gitlab}, path)
	wantError(t, err, "no value for GITLAB_TOKEN")
}

func TestReadSecretValuesRejectsADeclaredNameWithNoValue(t *testing.T) {
	path := writeSecretsFile(t, t.TempDir(), "GITLAB_TOKEN=\n")
	_, err := config.ReadSecretValues([]config.Secret{gitlab}, path)
	wantError(t, err, "no value for GITLAB_TOKEN")
}

func TestReadTokenStripsNewlines(t *testing.T) {
	if got := readToken(t, "abc123\n"); got != "abc123" {
		t.Fatalf("read %q", got)
	}
}

func TestReadTokenStripsAWindowsLineEnding(t *testing.T) {
	if got := readToken(t, "abc123\r\n"); got != "abc123" {
		t.Fatalf("read %q", got)
	}
}

func TestReadTokenStripsSurroundingSpaces(t *testing.T) {
	if got := readToken(t, "  abc123  "); got != "abc123" {
		t.Fatalf("read %q", got)
	}
}

func TestReadTokenRejectsAnEmptyFile(t *testing.T) {
	_, err := config.ReadToken(tokenFile(t, ""))
	wantError(t, err, "is empty")
}

func TestReadTokenRejectsAWhitespaceOnlyFile(t *testing.T) {
	_, err := config.ReadToken(tokenFile(t, "\n  \n"))
	wantError(t, err, "is empty")
}

func TestReadTokenRejectsAMissingFile(t *testing.T) {
	_, err := config.ReadToken(filepath.Join(t.TempDir(), "absent"))
	wantError(t, err, "does not exist")
}

// tokenFile writes a token file holding exactly the given text.
func tokenFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	writeText(t, path, contents)
	return path
}

// readToken reads a token file holding the given text, failing the test when box refuses it.
func readToken(t *testing.T, contents string) string {
	t.Helper()
	token, err := config.ReadToken(tokenFile(t, contents))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

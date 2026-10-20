package awsprofiles

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFiles(t *testing.T, creds, cfg string) {
	t.Helper()
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials")
	cfgPath := filepath.Join(dir, "config")
	if creds != "" {
		if err := os.WriteFile(credsPath, []byte(creds), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if cfg != "" {
		if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credsPath)
	t.Setenv("AWS_CONFIG_FILE", cfgPath)
}

func TestList(t *testing.T) {
	writeFiles(t, `
[dev]
aws_access_key_id = A
aws_secret_access_key = B

[shared]
aws_access_key_id = C
aws_secret_access_key = D
`, `
[default]
region = eu-west-1

[profile shared]
region = eu-south-2

[profile only-config]
region = us-east-1

[sso-session corp]
sso_region = eu-west-1

[services local]
s3 =
  endpoint_url = http://localhost
`)

	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"default", "dev", "only-config", "shared"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
}

func TestListMissingFiles(t *testing.T) {
	writeFiles(t, "", "")
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no profiles, got %v", got)
	}
}

func TestRegion(t *testing.T) {
	writeFiles(t, `
[dev]
region = ap-south-1
[noregion]
aws_access_key_id = A
`, `
[default]
region = eu-west-1
[profile shared]
region = eu-south-2
`)

	cases := map[string]string{
		"default":  "eu-west-1",
		"shared":   "eu-south-2",
		"dev":      "ap-south-1",
		"noregion": "",
		"missing":  "",
	}
	for profile, want := range cases {
		if got := Region(profile); got != want {
			t.Errorf("Region(%q) = %q, want %q", profile, got, want)
		}
	}
}

package secrets

import (
	"context"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

func TestFlatten(t *testing.T) {
	cases := []struct {
		name, secret, value string
		want                map[string]string
	}{
		{
			name:   "json object",
			secret: "app/dev",
			value:  `{"API_KEY":"abc","PORT":8080,"RATIO":0.5,"DEBUG":true,"EMPTY":null,"NESTED":{"a":[1,"<b>"]}}`,
			want: map[string]string{
				"API_KEY": "abc",
				"PORT":    "8080",
				"RATIO":   "0.5",
				"DEBUG":   "true",
				"EMPTY":   "",
				"NESTED":  `{"a":[1,"<b>"]}`,
			},
		},
		{
			name:   "plain text",
			secret: "my-app/db.pass",
			value:  "s3cr3t",
			want:   map[string]string{"MY_APP_DB_PASS": "s3cr3t"},
		},
		{
			name:   "json array is not an object",
			secret: "list",
			value:  `["a","b"]`,
			want:   map[string]string{"LIST": `["a","b"]`},
		},
		{
			name:   "json string",
			secret: "token",
			value:  `"quoted"`,
			want:   map[string]string{"TOKEN": `"quoted"`},
		},
		{
			name:   "trailing garbage",
			secret: "x",
			value:  `{"a":"b"} {"c":"d"}`,
			want:   map[string]string{"X": `{"a":"b"} {"c":"d"}`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Flatten(c.secret, c.value)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Flatten() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNormalizeKey(t *testing.T) {
	cases := map[string]string{
		"my-app/db_pass":  "MY_APP_DB_PASS",
		"prod//api--key":  "PROD_API_KEY",
		"/leading/":       "LEADING",
		"1password":       "_1PASSWORD",
		"---":             "SECRET",
		"Already_UPPER9":  "ALREADY_UPPER9",
		"spaces in name ": "SPACES_IN_NAME",
	}
	for in, want := range cases {
		if got := NormalizeKey(in); got != want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

type fakeAPI struct {
	pages  [][]types.SecretListEntry
	values map[string]*string
}

func (f *fakeAPI) ListSecrets(_ context.Context, in *secretsmanager.ListSecretsInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	i := 0
	if in.NextToken != nil {
		i = int((*in.NextToken)[0] - '0')
	}
	out := &secretsmanager.ListSecretsOutput{SecretList: f.pages[i]}
	if i+1 < len(f.pages) {
		out.NextToken = aws.String(string(rune('0' + i + 1)))
	}
	return out, nil
}

func (f *fakeAPI) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return &secretsmanager.GetSecretValueOutput{SecretString: f.values[*in.SecretId]}, nil
}

func TestListPaginatesFiltersAndSorts(t *testing.T) {
	api := &fakeAPI{pages: [][]types.SecretListEntry{
		{{Name: aws.String("zeta/dev")}, {Name: aws.String("alpha/dev"), Description: aws.String("Bruno API")}},
		{{Name: aws.String("beta/prod")}},
	}}

	all, err := List(context.Background(), api, "")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range all {
		names = append(names, s.Name)
	}
	if want := []string{"alpha/dev", "beta/prod", "zeta/dev"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}

	filtered, err := List(context.Background(), api, "bruno")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Name != "alpha/dev" {
		t.Fatalf("filter by description failed: %v", filtered)
	}
}

func TestGetBinary(t *testing.T) {
	api := &fakeAPI{values: map[string]*string{"ok": aws.String("v")}}
	if v, err := Get(context.Background(), api, "ok"); err != nil || v != "v" {
		t.Fatalf("Get(ok) = %q, %v", v, err)
	}
	if _, err := Get(context.Background(), api, "bin"); err == nil {
		t.Fatal("expected error for binary secret")
	}
}

// Package secrets reads secrets from AWS Secrets Manager and flattens them
// into environment variables.
package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// API is the subset of the Secrets Manager client used by this package.
type API interface {
	secretsmanager.ListSecretsAPIClient
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// Secret is a secret's metadata as shown to the user.
type Secret struct {
	Name        string
	Description string
}

// List returns every secret in the account/region, sorted by name. If filter
// is non-empty only secrets whose name or description contains it
// (case-insensitive) are returned.
func List(ctx context.Context, api API, filter string) ([]Secret, error) {
	filter = strings.ToLower(filter)
	var out []Secret
	p := secretsmanager.NewListSecretsPaginator(api, &secretsmanager.ListSecretsInput{MaxResults: aws.Int32(100)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, e := range page.SecretList {
			s := Secret{Name: aws.ToString(e.Name), Description: aws.ToString(e.Description)}
			if filter != "" &&
				!strings.Contains(strings.ToLower(s.Name), filter) &&
				!strings.Contains(strings.ToLower(s.Description), filter) {
				continue
			}
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns the string value of a secret.
func Get(ctx context.Context, api API, name string) (string, error) {
	res, err := api.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(name)})
	if err != nil {
		return "", err
	}
	if res.SecretString == nil {
		return "", fmt.Errorf("secret %q is binary; only string secrets are supported", name)
	}
	return *res.SecretString, nil
}

// Flatten converts a secret value into env vars. A JSON object yields one
// variable per key; anything else yields a single variable named after the
// secret (see NormalizeKey).
func Flatten(name, value string) map[string]string {
	dec := json.NewDecoder(strings.NewReader(value))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil || dec.More() {
		return map[string]string{NormalizeKey(name): value}
	}

	out := make(map[string]string, len(obj))
	for k, v := range obj {
		out[k] = stringify(v)
	}
	return out
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		// Nested objects/arrays are kept as compact JSON.
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(t)
		return strings.TrimSpace(buf.String())
	}
}

// NormalizeKey turns a secret name such as "my-app/db.pass" into a valid
// env var name ("MY_APP_DB_PASS").
func NormalizeKey(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	key := strings.Trim(b.String(), "_")
	if key == "" {
		return "SECRET"
	}
	if key[0] >= '0' && key[0] <= '9' {
		key = "_" + key
	}
	return key
}

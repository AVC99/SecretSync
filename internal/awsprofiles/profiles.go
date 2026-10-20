// Package awsprofiles lists the profiles defined in the AWS shared
// credentials and config files.
package awsprofiles

import (
	"os"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"gopkg.in/ini.v1"
)

// CredentialsFile returns the path of the shared credentials file,
// honoring AWS_SHARED_CREDENTIALS_FILE.
func CredentialsFile() string {
	if v := os.Getenv("AWS_SHARED_CREDENTIALS_FILE"); v != "" {
		return v
	}
	return config.DefaultSharedCredentialsFilename()
}

// ConfigFile returns the path of the shared config file, honoring AWS_CONFIG_FILE.
func ConfigFile() string {
	if v := os.Getenv("AWS_CONFIG_FILE"); v != "" {
		return v
	}
	return config.DefaultSharedConfigFilename()
}

func load(path string) (*ini.File, error) {
	// Loose: a missing file is not an error.
	return ini.LoadSources(ini.LoadOptions{Loose: true, AllowNestedValues: true}, path)
}

// List returns the sorted, de-duplicated profile names found in both files.
func List() ([]string, error) {
	seen := map[string]bool{}

	creds, err := load(CredentialsFile())
	if err != nil {
		return nil, err
	}
	for _, s := range creds.SectionStrings() {
		if s == ini.DefaultSection {
			continue
		}
		seen[strings.TrimSpace(s)] = true
	}

	cfg, err := load(ConfigFile())
	if err != nil {
		return nil, err
	}
	for _, s := range cfg.SectionStrings() {
		if name, ok := configSectionProfile(s); ok {
			seen[name] = true
		}
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		if n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

// configSectionProfile maps a config-file section name to a profile name.
// "[default]" -> default, "[profile x]" -> x; other sections (sso-session,
// services, ...) are not profiles.
func configSectionProfile(section string) (string, bool) {
	section = strings.TrimSpace(section)
	if section == "default" {
		return section, true
	}
	if rest, ok := strings.CutPrefix(section, "profile "); ok {
		return strings.TrimSpace(rest), true
	}
	return "", false
}

// Region returns the region configured for profile, looking first in the
// config file and then in the credentials file. Empty if none.
func Region(profile string) string {
	section := "profile " + profile
	if profile == "default" {
		section = "default"
	}
	if cfg, err := load(ConfigFile()); err == nil {
		if s, err := cfg.GetSection(section); err == nil {
			if r := s.Key("region").String(); r != "" {
				return r
			}
		}
		// "[default]" may also be written as "[profile default]".
		if profile == "default" {
			if s, err := cfg.GetSection("profile default"); err == nil {
				if r := s.Key("region").String(); r != "" {
					return r
				}
			}
		}
	}
	if creds, err := load(CredentialsFile()); err == nil {
		if s, err := creds.GetSection(profile); err == nil {
			return s.Key("region").String()
		}
	}
	return ""
}

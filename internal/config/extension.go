package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var extensionOrigin = regexp.MustCompile(`^(chrome-extension://[a-p]{32}|moz-extension://[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

func (c *Config) loadExtension(get func(string, string) string) error {
	raw := get("EXTENSION_ORIGINS", "")
	if raw != "" {
		for _, value := range strings.Split(raw, ",") {
			value = strings.TrimSpace(value)
			if !extensionOrigin.MatchString(value) {
				return fmt.Errorf("EXTENSION_ORIGINS must contain exact chrome-extension or moz-extension origins")
			}
			c.ExtensionOrigins = append(c.ExtensionOrigins, value)
		}
	}
	for _, field := range []struct {
		key, defaultValue string
		out               *int
	}{
		{"API_IP_PER_MINUTE", "120", &c.APIIPPerMinute}, {"API_OWNER_PER_MINUTE", "60", &c.APIOwnerPerMinute}, {"API_CLAIM_PER_MINUTE", "5", &c.APIClaimPerMinute},
	} {
		n, err := strconv.Atoi(get(field.key, field.defaultValue))
		if err != nil || n < 1 || n > 10000 {
			return fmt.Errorf("%s must be between 1 and 10000", field.key)
		}
		*field.out = n
	}
	return nil
}

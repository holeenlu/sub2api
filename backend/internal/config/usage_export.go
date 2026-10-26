package config

import "fmt"

// These bounds govern query work, not retention. Zero selects the documented
// default; administrators can request older history in bounded windows.
type UsageExportConfig struct {
	MaxRangeDays   int `mapstructure:"max_range_days"`
	MaxRows        int `mapstructure:"max_rows"`
	TimeoutSeconds int `mapstructure:"timeout_seconds"`
}

func (c UsageExportConfig) Effective() UsageExportConfig {
	if c.MaxRangeDays == 0 {
		c.MaxRangeDays = 90
	}
	if c.MaxRows == 0 {
		c.MaxRows = 100000
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 120
	}
	return c
}
func (c UsageExportConfig) Validate() error {
	c = c.Effective()
	if c.MaxRangeDays < 1 || c.MaxRangeDays > 3660 {
		return fmt.Errorf("usage_export.max_range_days must be between 1 and 3660")
	}
	if c.MaxRows < 1 || c.MaxRows > 1000000 {
		return fmt.Errorf("usage_export.max_rows must be between 1 and 1000000")
	}
	if c.TimeoutSeconds < 10 || c.TimeoutSeconds > 900 {
		return fmt.Errorf("usage_export.timeout_seconds must be between 10 and 900")
	}
	return nil
}

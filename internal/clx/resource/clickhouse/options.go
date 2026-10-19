package click_resources

import "alsayf/internal/clx/resource"

type ClickhouseOptions struct {
	resource.Options

	// Some default values for clickhouse
	Username string
	Password string
	Database string

	Query   string
	File    string
	Command string
	URL     string
	Headers []string

	Timeout int
}

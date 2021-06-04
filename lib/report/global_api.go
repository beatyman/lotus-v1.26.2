package report

var defaultReporter = NewReporter(100)

// set a report url, if noset, it will drop the data by sending.
func SetConfig(c *Config) {
	defaultReporter.SetConfig(c)
}

// unblock and send to the report buffer
func SendReport(data []byte) {
	defaultReporter.Send(data)
}

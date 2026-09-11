package configstate

import "strconv"

// Summary returns a fixed, input-free description of a diagnostic.
func Summary(d Diagnostic) string {
	message := diagnosticMessages[d.Stage][d.Reason]
	if message == "" {
		message = "Configuration could not be processed"
	}
	if d.Line > 0 {
		message += " (line " + strconv.Itoa(d.Line)
		if d.Column > 0 {
			message += ", column " + strconv.Itoa(d.Column)
		}
		message += ")"
	}
	return truncateUTF8(message, 256)
}

var diagnosticMessages = map[Stage]map[Reason]string{
	Read: {
		SourceUnavailable: "Configuration source is unavailable",
	},
	Migrate: {
		UnsupportedVersion: "Configuration version is unsupported",
	},
	Decode: {
		InvalidDocument: "Configuration document is invalid",
	},
	Validate: {
		InvalidConfiguration: "Configuration is invalid",
		MissingDependency:    "Configuration dependency is missing",
	},
	Apply: {
		RuntimeFailure: "Configuration runtime application failed",
	},
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	end := 0
	for index := range value {
		if index > limit {
			break
		}
		end = index
	}
	return value[:end]
}

package doctor

// AllChecks returns all registered doctor checks in execution order.
// To add a new check: implement the Check interface and add it here.
func AllChecks() []Check {
	return []Check{
		// Config checks
		&ConfigParseCheck{},
		&ConfigDeprecatedFieldsCheck{},
		&ConfigPredictRefCheck{},

		// Python checks
		&PydanticBaseModelCheck{},
		&DeprecatedImportsCheck{},

		// Environment checks
		&DockerCheck{},
	}
}

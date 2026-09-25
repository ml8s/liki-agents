package agent

// Structured output is an optional Agent capability. Plain text is the
// default so that generic chat Agents do not need an output artifact.
const (
	StructuredOutputNone       = ""
	StructuredOutputJSONSchema = "json_schema"
	StructuredOutputJSONObject = "json_object"
)

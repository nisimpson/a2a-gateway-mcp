package registry

// CallerSkill represents a skill on the caller agent card.
type CallerSkill struct {
	Name        string `json:"name" jsonschema:"skill name (required)"`
	Description string `json:"description,omitempty" jsonschema:"strongly recommended — provide one even if you need to infer it from the skill name"`
}

// CallerCapabilities describes supported A2A capabilities.
type CallerCapabilities struct {
	Streaming         bool `json:"streaming,omitempty" jsonschema:"whether the caller supports streaming"`
	PushNotifications bool `json:"pushNotifications,omitempty" jsonschema:"whether the caller supports push notifications"`
}

// CallerCard is the stored representation of the caller agent card.
type CallerCard struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	URL          string              `json:"url,omitempty"`
	Skills       []CallerSkill       `json:"skills,omitempty"`
	Capabilities *CallerCapabilities `json:"capabilities,omitempty"`
}

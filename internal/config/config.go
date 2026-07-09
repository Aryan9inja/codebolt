package config

type CodeBoltConfig struct {
	Version  int            `yaml:"version"`
	Analyzer AnalyzerConfig `yaml:"analyzer"`
	LLM      LLMConfig      `yaml:"llm"`
}

type AnalyzerConfig struct {
	Rules        []RuleConfig `yaml:"rules"`
	ExcludePaths []string     `yaml:"exclude_paths"`
}

type RuleConfig struct {
	Name    string `yaml:"name"`
	Enabled bool   `yaml:"enabled"`
}

type LLMConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

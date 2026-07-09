package embeddings

import "time"

// FindingRecord is the unit stored and retrieved from the vector store.
// Only llm.EnhancedFinding output gets embedded - AST findings are
// deterministic/reproducible, there's no pattern-similarity value in them.
type FindingRecord struct {
	ID        string
	Repo      string
	FilePath  string
	Rule      string
	Message   string
	PRNumber  int
	Embedding []float32
	CreatedAt time.Time
}

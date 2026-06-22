package embeddings

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("embeddings: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("embeddings: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) SaveFinding(ctx context.Context, rec FindingRecord) error {
	vec := pgvector.NewVector(rec.Embedding)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO finding_embeddings (id, repo, file_path, rule, message, embedding, pr_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, uuid.New(), rec.Repo, rec.FilePath, rec.Rule, rec.Message, vec, rec.PRNumber)
	if err != nil {
		return fmt.Errorf("embeddings: save finding: %w", err)
	}
	return nil
}

// SearchSimilar returns the topK most similar past findings in the same repo,
// ranked by cosine distance. Scoped to repo so patterns don't leak cross-tenant.
func (s *Store) SearchSimilar(ctx context.Context, repo string, embedding []float32, topK int) ([]FindingRecord, error) {
	vec := pgvector.NewVector(embedding)
	rows, err := s.pool.Query(ctx, `
		SELECT id, repo, file_path, rule, message, pr_number, created_at
		FROM finding_embeddings
		WHERE repo = $1
		ORDER BY embedding <=> $2
		LIMIT $3
	`, repo, vec, topK)
	if err != nil {
		return nil, fmt.Errorf("embeddings: search similar: %w", err)
	}
	defer rows.Close()

	var results []FindingRecord
	for rows.Next() {
		var rec FindingRecord
		if err := rows.Scan(&rec.ID, &rec.Repo, &rec.FilePath, &rec.Rule, &rec.Message, &rec.PRNumber, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("embeddings: scan: %w", err)
		}
		results = append(results, rec)
	}
	return results, rows.Err()
}
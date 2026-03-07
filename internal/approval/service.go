package approval

import (
	"context"

	"finance-sys/internal/repository"
)

type Service struct {
	repo *repository.Repository
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ApprovePlan(ctx context.Context, id int64, approvedBy string) error {
	_, err := s.repo.ApprovePlan(ctx, id, approvedBy)
	return err
}

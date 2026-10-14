package service

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
)

// MemberLabel is how a patrol participant is shown: "Name (Block)" or just the block.
func MemberLabel(m db.Member) string {
	return domain.DisplayName(m.Name, m.HouseBlock)
}

// MemberInput is the raw form input for creating or updating a member.
type MemberInput struct {
	HouseBlock, Name, Phone, Notes string
}

func (in MemberInput) validate() ValidationErrors {
	if strings.TrimSpace(in.HouseBlock) == "" {
		return ValidationErrors{"house_block": "Blok rumah wajib diisi."}
	}
	return nil
}

// normalizedBlock stores blocks upper-cased and trimmed, e.g. " c-10 " -> "C-10".
func normalizedBlock(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

type MemberService struct {
	pool *pgxpool.Pool
}

func NewMemberService(pool *pgxpool.Pool) *MemberService { return &MemberService{pool: pool} }

func (s *MemberService) List(ctx context.Context) ([]db.Member, error) {
	return db.New(s.pool).ListMembers(ctx)
}

func (s *MemberService) ListActive(ctx context.Context) ([]db.Member, error) {
	return db.New(s.pool).ListActiveMembers(ctx)
}

func (s *MemberService) Get(ctx context.Context, id int64) (db.Member, error) {
	m, err := db.New(s.pool).GetMember(ctx, id)
	return m, notFound(err)
}

func (s *MemberService) Create(ctx context.Context, in MemberInput) (db.Member, error) {
	if err := in.validate(); err != nil {
		return db.Member{}, err
	}
	return db.New(s.pool).CreateMember(ctx, db.CreateMemberParams{
		HouseBlock: normalizedBlock(in.HouseBlock), Name: optional(in.Name), Phone: optional(in.Phone), Notes: optional(in.Notes),
	})
}

func (s *MemberService) Update(ctx context.Context, id int64, in MemberInput) (db.Member, error) {
	if err := in.validate(); err != nil {
		return db.Member{}, err
	}
	m, err := db.New(s.pool).UpdateMember(ctx, db.UpdateMemberParams{
		ID: id, HouseBlock: normalizedBlock(in.HouseBlock), Name: optional(in.Name), Phone: optional(in.Phone), Notes: optional(in.Notes),
	})
	return m, notFound(err)
}

// SetActive deactivates or reactivates a member. Members are never deleted: they
// stay in old recaps, and inactive members are left out of new teams.
func (s *MemberService) SetActive(ctx context.Context, id int64, active bool) error {
	q := db.New(s.pool)
	if _, err := q.GetMember(ctx, id); err != nil {
		return notFound(err)
	}
	return q.SetMemberActive(ctx, db.SetMemberActiveParams{ID: id, Active: active})
}

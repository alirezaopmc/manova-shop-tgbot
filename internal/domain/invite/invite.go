package invite

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInviteNotFound = errors.New("invite not found")
	ErrInviteUsed     = errors.New("invite has reached its maximum uses or is inactive")
	ErrInviteExpired  = errors.New("invite has expired")
	ErrInviteSelf     = errors.New("cannot redeem your own invite")
)

// Invite models an onboarding token that permits users to join the bot.
type Invite struct {
	Code      string
	CreatedBy int64
	MaxUses   int // 0 means unlimited
	UsesCount int
	ExpiresAt *time.Time
	IsActive  bool
	CreatedAt time.Time
}

// GenerateCode creates an 8-character random hex code.
func GenerateCode() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()%0xFFFFFFFF)
	}
	return hex.EncodeToString(b)
}

// New creates an invite with specified creator, usage limit, and optional validity duration.
func New(createdBy int64, maxUses int, duration *time.Duration, now time.Time) *Invite {
	code := GenerateCode()
	var expiresAt *time.Time
	if duration != nil && *duration > 0 {
		exp := now.Add(*duration)
		expiresAt = &exp
	}

	return &Invite{
		Code:      code,
		CreatedBy: createdBy,
		MaxUses:   maxUses,
		UsesCount: 0,
		ExpiresAt: expiresAt,
		IsActive:  true,
		CreatedAt: now,
	}
}

// Redeemable checks whether the user can claim this invite without mutating state.
func (i *Invite) Redeemable(userID int64, now time.Time) error {
	if i.CreatedBy == userID {
		return ErrInviteSelf
	}
	if !i.IsActive {
		return ErrInviteUsed
	}
	if i.MaxUses > 0 && i.UsesCount >= i.MaxUses {
		return ErrInviteUsed
	}
	if i.ExpiresAt != nil && now.After(*i.ExpiresAt) {
		return ErrInviteExpired
	}
	return nil
}

// Claim records one redemption of the invite.
func (i *Invite) Claim(userID int64, now time.Time) error {
	if err := i.Redeemable(userID, now); err != nil {
		return err
	}
	i.UsesCount++
	if i.MaxUses > 0 && i.UsesCount >= i.MaxUses {
		i.IsActive = false
	}
	return nil
}

// CleanCode extracts the raw invite code from text or deep link (e.g. "/start invite_abc" -> "abc").
func CleanCode(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "/start")
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "invite_") {
		s = strings.TrimPrefix(s, "invite_")
	} else if strings.HasPrefix(s, "ref_") {
		s = strings.TrimPrefix(s, "ref_")
	}
	return strings.ToLower(strings.TrimSpace(s))
}

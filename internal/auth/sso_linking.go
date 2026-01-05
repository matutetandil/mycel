package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Account linking errors
var (
	ErrAccountAlreadyLinked = errors.New("account already linked to another user")
	ErrNoMatchingAccount    = errors.New("no matching account found")
	ErrLinkingDisabled      = errors.New("account linking is disabled")
	ErrVerificationRequired = errors.New("email verification required for linking")
)

// LinkedAccount represents a social/SSO account linked to a user
type LinkedAccount struct {
	ID           string
	UserID       string
	Provider     string
	ProviderID   string
	Email        string
	Name         string
	Picture      string
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Metadata     map[string]interface{}
}

// LinkedAccountStore interface for storing linked accounts
type LinkedAccountStore interface {
	// Create creates a new linked account
	Create(ctx context.Context, account *LinkedAccount) error

	// FindByProviderID finds a linked account by provider and provider ID
	FindByProviderID(ctx context.Context, provider, providerID string) (*LinkedAccount, error)

	// FindByUserID finds all linked accounts for a user
	FindByUserID(ctx context.Context, userID string) ([]*LinkedAccount, error)

	// FindByEmail finds linked accounts by email
	FindByEmail(ctx context.Context, email string) ([]*LinkedAccount, error)

	// Update updates a linked account
	Update(ctx context.Context, account *LinkedAccount) error

	// Delete deletes a linked account
	Delete(ctx context.Context, id string) error

	// DeleteByUserID deletes all linked accounts for a user
	DeleteByUserID(ctx context.Context, userID string) error
}

// AccountLinkingService handles account linking logic
type AccountLinkingService struct {
	config    *AccountLinkingConfig
	store     LinkedAccountStore
	userStore UserStore
	mu        sync.RWMutex
}

// NewAccountLinkingService creates a new account linking service
func NewAccountLinkingService(config *AccountLinkingConfig, store LinkedAccountStore, userStore UserStore) *AccountLinkingService {
	if config == nil {
		config = &AccountLinkingConfig{
			Enabled:             true,
			MatchBy:             "email",
			RequireVerification: true,
			OnMatch:             "link",
		}
	}

	return &AccountLinkingService{
		config:    config,
		store:     store,
		userStore: userStore,
	}
}

// LinkResult represents the result of a link attempt
type LinkResult struct {
	// Action taken: created, linked, prompt
	Action string

	// User is the user account (existing or new)
	User *User

	// LinkedAccount is the newly created or updated linked account
	LinkedAccount *LinkedAccount

	// NeedsConfirmation indicates user should confirm linking
	NeedsConfirmation bool

	// ExistingAccounts are accounts found for potential linking
	ExistingAccounts []*LinkedAccount
}

// LinkOrCreate links a social account to an existing user or creates a new user
func (s *AccountLinkingService) LinkOrCreate(ctx context.Context, userInfo *OAuth2UserInfo, token *OAuth2Token) (*LinkResult, error) {
	if !s.config.Enabled {
		return nil, ErrLinkingDisabled
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if this provider account is already linked
	existing, err := s.store.FindByProviderID(ctx, userInfo.Provider, userInfo.ID)
	if err == nil && existing != nil {
		// Already linked - return the existing user
		user, err := s.userStore.FindByID(ctx, existing.UserID)
		if err != nil {
			return nil, fmt.Errorf("failed to find linked user: %w", err)
		}

		// Update tokens
		existing.AccessToken = token.AccessToken
		existing.RefreshToken = token.RefreshToken
		if token.ExpiresIn > 0 {
			exp := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
			existing.ExpiresAt = &exp
		}
		existing.UpdatedAt = time.Now()
		_ = s.store.Update(ctx, existing)

		return &LinkResult{
			Action:        "existing",
			User:          user,
			LinkedAccount: existing,
		}, nil
	}

	// Try to find matching user
	matchedUser, err := s.findMatchingUser(ctx, userInfo)
	if err != nil && err != ErrNoMatchingAccount {
		return nil, err
	}

	// Create linked account
	linkedAccount := &LinkedAccount{
		ID:           generateAccountID(),
		Provider:     userInfo.Provider,
		ProviderID:   userInfo.ID,
		Email:        userInfo.Email,
		Name:         userInfo.Name,
		Picture:      userInfo.Picture,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Metadata:     userInfo.Raw,
	}
	if token.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
		linkedAccount.ExpiresAt = &exp
	}

	if matchedUser != nil {
		// Found matching user
		switch s.config.OnMatch {
		case "link":
			// Auto-link if verification requirements are met
			if s.config.RequireVerification && !userInfo.EmailVerified {
				return nil, ErrVerificationRequired
			}

			linkedAccount.UserID = matchedUser.ID
			if err := s.store.Create(ctx, linkedAccount); err != nil {
				return nil, fmt.Errorf("failed to create linked account: %w", err)
			}

			return &LinkResult{
				Action:        "linked",
				User:          matchedUser,
				LinkedAccount: linkedAccount,
			}, nil

		case "prompt":
			// Return existing accounts for user confirmation
			existingAccounts, _ := s.store.FindByUserID(ctx, matchedUser.ID)
			return &LinkResult{
				Action:            "prompt",
				User:              matchedUser,
				NeedsConfirmation: true,
				ExistingAccounts:  existingAccounts,
			}, nil

		case "reject":
			return nil, ErrAccountAlreadyLinked
		}
	}

	// No matching user - create new user
	newUser, err := s.createUserFromProvider(ctx, userInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	linkedAccount.UserID = newUser.ID
	if err := s.store.Create(ctx, linkedAccount); err != nil {
		return nil, fmt.Errorf("failed to create linked account: %w", err)
	}

	return &LinkResult{
		Action:        "created",
		User:          newUser,
		LinkedAccount: linkedAccount,
	}, nil
}

// findMatchingUser finds a user that matches the provider info
func (s *AccountLinkingService) findMatchingUser(ctx context.Context, userInfo *OAuth2UserInfo) (*User, error) {
	switch s.config.MatchBy {
	case "email":
		if userInfo.Email == "" {
			return nil, ErrNoMatchingAccount
		}
		user, err := s.userStore.FindByEmail(ctx, userInfo.Email)
		if err != nil {
			return nil, ErrNoMatchingAccount
		}
		return user, nil

	case "none":
		return nil, ErrNoMatchingAccount

	default:
		// Custom matching could be implemented here
		return nil, ErrNoMatchingAccount
	}
}

// createUserFromProvider creates a new user from provider info
func (s *AccountLinkingService) createUserFromProvider(ctx context.Context, userInfo *OAuth2UserInfo) (*User, error) {
	userID, err := generateID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	user := &User{
		ID:        userID,
		Email:     userInfo.Email,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata: map[string]interface{}{
			"provider":     userInfo.Provider,
			"provider_id":  userInfo.ID,
			"name":         userInfo.Name,
			"picture":      userInfo.Picture,
			"created_via":  "social_login",
			"given_name":   userInfo.GivenName,
			"family_name":  userInfo.FamilyName,
		},
	}

	// No password for social-only accounts
	// PasswordHash is empty, user must use social login

	if err := s.userStore.Create(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// ConfirmLink confirms linking when user approval is required
func (s *AccountLinkingService) ConfirmLink(ctx context.Context, userID string, userInfo *OAuth2UserInfo, token *OAuth2Token) (*LinkedAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify user exists
	user, err := s.userStore.FindByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Check if already linked
	existing, _ := s.store.FindByProviderID(ctx, userInfo.Provider, userInfo.ID)
	if existing != nil && existing.UserID != userID {
		return nil, ErrAccountAlreadyLinked
	}

	// Create linked account
	linkedAccount := &LinkedAccount{
		ID:           generateAccountID(),
		UserID:       user.ID,
		Provider:     userInfo.Provider,
		ProviderID:   userInfo.ID,
		Email:        userInfo.Email,
		Name:         userInfo.Name,
		Picture:      userInfo.Picture,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Metadata:     userInfo.Raw,
	}
	if token.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
		linkedAccount.ExpiresAt = &exp
	}

	if err := s.store.Create(ctx, linkedAccount); err != nil {
		return nil, fmt.Errorf("failed to create linked account: %w", err)
	}

	return linkedAccount, nil
}

// Unlink removes a linked account
func (s *AccountLinkingService) Unlink(ctx context.Context, userID, provider string) error {
	accounts, err := s.store.FindByUserID(ctx, userID)
	if err != nil {
		return err
	}

	// Find the account to unlink
	for _, acc := range accounts {
		if acc.Provider == provider {
			// Check if this is the only login method
			user, err := s.userStore.FindByID(ctx, userID)
			if err != nil {
				return err
			}

			// If no password and only one linked account, can't unlink
			if user.PasswordHash == "" && len(accounts) == 1 {
				return errors.New("cannot unlink the only authentication method")
			}

			return s.store.Delete(ctx, acc.ID)
		}
	}

	return errors.New("linked account not found")
}

// GetLinkedAccounts returns all linked accounts for a user
func (s *AccountLinkingService) GetLinkedAccounts(ctx context.Context, userID string) ([]*LinkedAccount, error) {
	return s.store.FindByUserID(ctx, userID)
}

// MemoryLinkedAccountStore implements LinkedAccountStore in memory
type MemoryLinkedAccountStore struct {
	accounts map[string]*LinkedAccount
	mu       sync.RWMutex
}

// NewMemoryLinkedAccountStore creates a new memory store
func NewMemoryLinkedAccountStore() *MemoryLinkedAccountStore {
	return &MemoryLinkedAccountStore{
		accounts: make(map[string]*LinkedAccount),
	}
}

func (s *MemoryLinkedAccountStore) Create(ctx context.Context, account *LinkedAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate provider+providerID
	for _, acc := range s.accounts {
		if acc.Provider == account.Provider && acc.ProviderID == account.ProviderID {
			return ErrAccountAlreadyLinked
		}
	}

	s.accounts[account.ID] = account
	return nil
}

func (s *MemoryLinkedAccountStore) FindByProviderID(ctx context.Context, provider, providerID string) (*LinkedAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, acc := range s.accounts {
		if acc.Provider == provider && acc.ProviderID == providerID {
			return acc, nil
		}
	}
	return nil, errors.New("linked account not found")
}

func (s *MemoryLinkedAccountStore) FindByUserID(ctx context.Context, userID string) ([]*LinkedAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*LinkedAccount
	for _, acc := range s.accounts {
		if acc.UserID == userID {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (s *MemoryLinkedAccountStore) FindByEmail(ctx context.Context, email string) ([]*LinkedAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*LinkedAccount
	for _, acc := range s.accounts {
		if acc.Email == email {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (s *MemoryLinkedAccountStore) Update(ctx context.Context, account *LinkedAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.accounts[account.ID]; !exists {
		return errors.New("linked account not found")
	}

	s.accounts[account.ID] = account
	return nil
}

func (s *MemoryLinkedAccountStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.accounts, id)
	return nil
}

func (s *MemoryLinkedAccountStore) DeleteByUserID(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, acc := range s.accounts {
		if acc.UserID == userID {
			delete(s.accounts, id)
		}
	}
	return nil
}

func generateAccountID() string {
	id, _ := generateID()
	return id
}

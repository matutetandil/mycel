package auth

import (
	"context"
	"errors"
	"sync"
	"time"
)

// UserStore interface for user storage operations
type UserStore interface {
	// FindByID finds a user by ID
	FindByID(ctx context.Context, id string) (*User, error)

	// FindByEmail finds a user by email
	FindByEmail(ctx context.Context, email string) (*User, error)

	// Create creates a new user
	Create(ctx context.Context, user *User) error

	// Update updates an existing user
	Update(ctx context.Context, user *User) error

	// Delete deletes a user
	Delete(ctx context.Context, id string) error

	// UpdatePassword updates user's password hash
	UpdatePassword(ctx context.Context, id string, passwordHash string) error

	// UpdateLastLogin updates the last login timestamp
	UpdateLastLogin(ctx context.Context, id string, t time.Time) error
}

// SessionStore interface for session storage operations
type SessionStore interface {
	// Create creates a new session
	Create(ctx context.Context, session *Session) error

	// FindByID finds a session by ID
	FindByID(ctx context.Context, id string) (*Session, error)

	// FindByUserID finds all sessions for a user
	FindByUserID(ctx context.Context, userID string) ([]*Session, error)

	// Update updates a session
	Update(ctx context.Context, session *Session) error

	// Delete deletes a session
	Delete(ctx context.Context, id string) error

	// DeleteByUserID deletes all sessions for a user
	DeleteByUserID(ctx context.Context, userID string) error

	// DeleteExpired deletes expired sessions
	DeleteExpired(ctx context.Context) error

	// Count counts active sessions for a user
	Count(ctx context.Context, userID string) (int, error)

	// Touch updates the last active time
	Touch(ctx context.Context, id string) error
}

// TokenStore interface for token blacklist/replay protection
type TokenStore interface {
	// Add adds a token to the store (for blacklist or replay protection)
	Add(ctx context.Context, tokenID string, expiry time.Time) error

	// Exists checks if a token exists in the store
	Exists(ctx context.Context, tokenID string) (bool, error)

	// Delete removes a token from the store
	Delete(ctx context.Context, tokenID string) error

	// Cleanup removes expired tokens
	Cleanup(ctx context.Context) error
}

// BruteForceStore interface for tracking failed login attempts
type BruteForceStore interface {
	// Increment increments the failure count for a key
	Increment(ctx context.Context, key string, window time.Duration) (int, error)

	// Get gets the current failure count for a key
	Get(ctx context.Context, key string) (int, error)

	// GetAttempts is an alias for Get (for compatibility)
	GetAttempts(ctx context.Context, key string) (int, error)

	// Reset resets the failure count for a key
	Reset(ctx context.Context, key string) error

	// IsLocked checks if a key is locked
	IsLocked(ctx context.Context, key string) (bool, time.Time, error)

	// Lock locks a key for a duration
	Lock(ctx context.Context, key string, duration time.Duration) error

	// GetDelay returns the current progressive delay for a key
	GetDelay(ctx context.Context, key string) (time.Duration, error)

	// SetDelay sets the progressive delay for a key
	SetDelay(ctx context.Context, key string, delay time.Duration, window time.Duration) error
}

// MemoryUserStore implements UserStore in memory (for development/testing)
type MemoryUserStore struct {
	mu    sync.RWMutex
	users map[string]*User
	byEmail map[string]string // email -> id
}

// NewMemoryUserStore creates a new in-memory user store
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{
		users:   make(map[string]*User),
		byEmail: make(map[string]string),
	}
}

func (s *MemoryUserStore) FindByID(ctx context.Context, id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return copyUser(user), nil
}

func (s *MemoryUserStore) FindByEmail(ctx context.Context, email string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byEmail[email]
	if !ok {
		return nil, ErrUserNotFound
	}
	user, ok := s.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return copyUser(user), nil
}

func (s *MemoryUserStore) Create(ctx context.Context, user *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.byEmail[user.Email]; exists {
		return ErrUserExists
	}

	s.users[user.ID] = copyUser(user)
	s.byEmail[user.Email] = user.ID
	return nil
}

func (s *MemoryUserStore) Update(ctx context.Context, user *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[user.ID]; !exists {
		return ErrUserNotFound
	}

	// Update email index if changed
	oldUser := s.users[user.ID]
	if oldUser.Email != user.Email {
		delete(s.byEmail, oldUser.Email)
		s.byEmail[user.Email] = user.ID
	}

	s.users[user.ID] = copyUser(user)
	return nil
}

func (s *MemoryUserStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[id]
	if !exists {
		return ErrUserNotFound
	}

	delete(s.byEmail, user.Email)
	delete(s.users, id)
	return nil
}

func (s *MemoryUserStore) UpdatePassword(ctx context.Context, id string, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[id]
	if !exists {
		return ErrUserNotFound
	}

	user.PasswordHash = passwordHash
	user.UpdatedAt = time.Now()
	return nil
}

func (s *MemoryUserStore) UpdateLastLogin(ctx context.Context, id string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[id]
	if !exists {
		return ErrUserNotFound
	}

	user.LastLoginAt = &t
	return nil
}

func copyUser(u *User) *User {
	copy := *u
	if u.Roles != nil {
		copy.Roles = make([]string, len(u.Roles))
		copy2 := copy
		copy2.Roles = append(copy2.Roles[:0:0], u.Roles...)
		copy = copy2
	}
	if u.Permissions != nil {
		copy.Permissions = make([]string, len(u.Permissions))
		copy2 := copy
		copy2.Permissions = append(copy2.Permissions[:0:0], u.Permissions...)
		copy = copy2
	}
	return &copy
}

// MemorySessionStore implements SessionStore in memory
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	byUser   map[string][]string // userID -> []sessionID
}

// NewMemorySessionStore creates a new in-memory session store
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{
		sessions: make(map[string]*Session),
		byUser:   make(map[string][]string),
	}
}

func (s *MemorySessionStore) Create(ctx context.Context, session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[session.ID] = copySession(session)
	s.byUser[session.UserID] = append(s.byUser[session.UserID], session.ID)
	return nil
}

func (s *MemorySessionStore) FindByID(ctx context.Context, id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	return copySession(session), nil
}

func (s *MemorySessionStore) FindByUserID(ctx context.Context, userID string) ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.byUser[userID]
	sessions := make([]*Session, 0, len(ids))
	for _, id := range ids {
		if session, ok := s.sessions[id]; ok {
			sessions = append(sessions, copySession(session))
		}
	}
	return sessions, nil
}

func (s *MemorySessionStore) Update(ctx context.Context, session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[session.ID]; !exists {
		return errors.New("session not found")
	}
	s.sessions[session.ID] = copySession(session)
	return nil
}

func (s *MemorySessionStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[id]
	if !exists {
		return nil
	}

	// Remove from user index
	userSessions := s.byUser[session.UserID]
	for i, sid := range userSessions {
		if sid == id {
			s.byUser[session.UserID] = append(userSessions[:i], userSessions[i+1:]...)
			break
		}
	}

	delete(s.sessions, id)
	return nil
}

func (s *MemorySessionStore) DeleteByUserID(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := s.byUser[userID]
	for _, id := range ids {
		delete(s.sessions, id)
	}
	delete(s.byUser, userID)
	return nil
}

func (s *MemorySessionStore) DeleteExpired(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, session := range s.sessions {
		if session.ExpiresAt.Before(now) {
			// Remove from user index
			userSessions := s.byUser[session.UserID]
			for i, sid := range userSessions {
				if sid == id {
					s.byUser[session.UserID] = append(userSessions[:i], userSessions[i+1:]...)
					break
				}
			}
			delete(s.sessions, id)
		}
	}
	return nil
}

func (s *MemorySessionStore) Count(ctx context.Context, userID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.byUser[userID]), nil
}

func (s *MemorySessionStore) Touch(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[id]
	if !exists {
		return errors.New("session not found")
	}
	session.LastActiveAt = time.Now()
	return nil
}

func copySession(s *Session) *Session {
	copy := *s
	return &copy
}

// MemoryTokenStore implements TokenStore in memory
type MemoryTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]time.Time // tokenID -> expiry
}

// NewMemoryTokenStore creates a new in-memory token store
func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{
		tokens: make(map[string]time.Time),
	}
}

func (s *MemoryTokenStore) Add(ctx context.Context, tokenID string, expiry time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tokens[tokenID] = expiry
	return nil
}

func (s *MemoryTokenStore) Exists(ctx context.Context, tokenID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	expiry, exists := s.tokens[tokenID]
	if !exists {
		return false, nil
	}

	// Check if expired
	if time.Now().After(expiry) {
		return false, nil
	}

	return true, nil
}

func (s *MemoryTokenStore) Delete(ctx context.Context, tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tokens, tokenID)
	return nil
}

func (s *MemoryTokenStore) Cleanup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, expiry := range s.tokens {
		if now.After(expiry) {
			delete(s.tokens, id)
		}
	}
	return nil
}

// MemoryBruteForceStore implements BruteForceStore in memory
type MemoryBruteForceStore struct {
	mu       sync.RWMutex
	attempts map[string]*bruteForceEntry
}

type bruteForceEntry struct {
	count     int
	firstAt   time.Time
	lockedAt  time.Time
	lockUntil time.Time
	delay     time.Duration
}

// NewMemoryBruteForceStore creates a new in-memory brute force store
func NewMemoryBruteForceStore() *MemoryBruteForceStore {
	return &MemoryBruteForceStore{
		attempts: make(map[string]*bruteForceEntry),
	}
}

func (s *MemoryBruteForceStore) Increment(ctx context.Context, key string, window time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	entry, exists := s.attempts[key]

	if !exists || now.Sub(entry.firstAt) > window {
		// Start new window
		s.attempts[key] = &bruteForceEntry{
			count:   1,
			firstAt: now,
		}
		return 1, nil
	}

	entry.count++
	return entry.count, nil
}

func (s *MemoryBruteForceStore) Get(ctx context.Context, key string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.attempts[key]
	if !exists {
		return 0, nil
	}
	return entry.count, nil
}

func (s *MemoryBruteForceStore) Reset(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.attempts, key)
	return nil
}

func (s *MemoryBruteForceStore) IsLocked(ctx context.Context, key string) (bool, time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.attempts[key]
	if !exists {
		return false, time.Time{}, nil
	}

	now := time.Now()
	if entry.lockUntil.After(now) {
		return true, entry.lockUntil, nil
	}

	return false, time.Time{}, nil
}

func (s *MemoryBruteForceStore) Lock(ctx context.Context, key string, duration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	entry, exists := s.attempts[key]
	if !exists {
		entry = &bruteForceEntry{}
		s.attempts[key] = entry
	}

	entry.lockedAt = now
	entry.lockUntil = now.Add(duration)
	return nil
}

// GetAttempts is an alias for Get
func (s *MemoryBruteForceStore) GetAttempts(ctx context.Context, key string) (int, error) {
	return s.Get(ctx, key)
}

// GetDelay returns the current progressive delay for a key
func (s *MemoryBruteForceStore) GetDelay(ctx context.Context, key string) (time.Duration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.attempts[key]
	if !exists {
		return 0, nil
	}
	return entry.delay, nil
}

// SetDelay sets the progressive delay for a key
func (s *MemoryBruteForceStore) SetDelay(ctx context.Context, key string, delay time.Duration, window time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.attempts[key]
	if !exists {
		entry = &bruteForceEntry{
			firstAt: time.Now(),
		}
		s.attempts[key] = entry
	}

	entry.delay = delay
	return nil
}
